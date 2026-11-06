package core

import (
	"strings"

	"github.com/system-inc/cohere/internal/lint/ecmascript/regex_syntax"
)

// regexStructureScanner walks a pattern once, keeping a stack of what encloses the current byte.
type regexStructureScanner struct {
	pattern   string
	flags     regex_syntax.RegexFlags
	structure regexStructure
	// stack holds the indices of the currently open nodes, outermost first.
	stack []int
	// groupStack records, for each open `(`, the group index it opened or -1 for a non-capturing
	// one, so the closing `)` can finish the right group's span.
	groupStack []int
	// alternativeStack records the alternative node index open inside each enclosing group, so a
	// `|` can close one branch and open the next at the right level.
	alternativeStack []int
}

// push adds a node and returns its index.
func (scanner *regexStructureScanner) push(kind regexNodeKind, negative bool) int {
	scanner.structure.nodes = append(scanner.structure.nodes, regexNode{kind: kind, negative: negative})
	index := len(scanner.structure.nodes) - 1
	scanner.stack = append(scanner.stack, index)
	return index
}

// pop removes the innermost open node.
func (scanner *regexStructureScanner) pop() {
	if len(scanner.stack) > 0 {
		scanner.stack = scanner.stack[:len(scanner.stack)-1]
	}
}

// currentPath copies the open stack, which is what a group or reference records about itself.
func (scanner *regexStructureScanner) currentPath() []int {
	path := make([]int, len(scanner.stack))
	copy(path, scanner.stack)
	return path
}

func (scanner *regexStructureScanner) run() bool {
	pattern := scanner.pattern

	// The root node stands for the whole pattern, so every path shares index 0 and the walk that
	// finds the lowest common ancestor always has somewhere to start.
	scanner.push(regexNodeGroup, false)
	scanner.openAlternative()

	for index := 0; index < len(pattern); {
		switch pattern[index] {
		case '\\':
			step, ok := scanner.readEscape(index)
			if !ok {
				return false
			}
			index += step

		case '[':
			// Everything inside a character class is a literal, which is the whole of
			// `/^[\1](a)$/` being clean: the `\1` there matches a control character, not a group.
			end, ok := regex_syntax.ClassEnd(pattern, index, scanner.flags)
			if !ok {
				return false
			}
			index = end

		case '(':
			index = scanner.openGroup(index)

		case ')':
			if !scanner.closeGroup(index) {
				return false
			}
			index++

		case '|':
			scanner.closeAlternative()
			scanner.openAlternative()
			index++

		default:
			index++
		}
	}

	// A pattern whose parentheses do not balance is a syntax error. The root group and its
	// alternative are the only two nodes that should still be open.
	return len(scanner.groupStack) == 0
}

// openAlternative starts a new branch at the current level.
func (scanner *regexStructureScanner) openAlternative() {
	index := scanner.push(regexNodeAlternative, false)
	scanner.alternativeStack = append(scanner.alternativeStack, index)
}

// closeAlternative ends the branch at the current level.
func (scanner *regexStructureScanner) closeAlternative() {
	if len(scanner.alternativeStack) > 0 {
		scanner.alternativeStack = scanner.alternativeStack[:len(scanner.alternativeStack)-1]
	}
	scanner.pop()
}

// openGroup handles a `(` and returns the index just past whatever prefix it consumed.
func (scanner *regexStructureScanner) openGroup(index int) int {
	pattern := scanner.pattern
	kind := regexNodeCapturingGroup
	negative := false
	name := ""
	next := index + 1

	if next < len(pattern) && pattern[next] == '?' {
		switch {
		case next+1 < len(pattern) && pattern[next+1] == ':':
			kind, next = regexNodeGroup, next+2
		case next+1 < len(pattern) && pattern[next+1] == '=':
			kind, next = regexNodeLookahead, next+2
		case next+1 < len(pattern) && pattern[next+1] == '!':
			kind, negative, next = regexNodeLookahead, true, next+2
		case next+2 < len(pattern) && pattern[next+1] == '<' && pattern[next+2] == '=':
			kind, next = regexNodeLookbehind, next+3
		case next+2 < len(pattern) && pattern[next+1] == '<' && pattern[next+2] == '!':
			kind, negative, next = regexNodeLookbehind, true, next+3
		case next+1 < len(pattern) && pattern[next+1] == '<':
			// `(?<name>` is the one `(?<` form that captures.
			closing := strings.IndexByte(pattern[next+2:], '>')
			if closing < 0 {
				kind, next = regexNodeGroup, next+2
			} else {
				name = pattern[next+2 : next+2+closing]
				next = next + 2 + closing + 1
			}
		default:
			// A `(?` this scanner does not know is treated as a plain nesting group rather than
			// guessed at, so the paths stay right even if the construct is exotic.
			kind, next = regexNodeGroup, next+2
		}
	}

	scanner.push(kind, negative)

	if kind == regexNodeCapturingGroup {
		scanner.structure.groups = append(scanner.structure.groups, capturingGroup{
			name:  name,
			start: index,
			path:  scanner.currentPath(),
		})
		scanner.groupStack = append(scanner.groupStack, len(scanner.structure.groups)-1)
	} else {
		scanner.groupStack = append(scanner.groupStack, -1)
	}

	scanner.openAlternative()
	return next
}

// closeGroup handles a `)`, finishing the group's span if it captured.
func (scanner *regexStructureScanner) closeGroup(index int) bool {
	if len(scanner.groupStack) == 0 {
		return false
	}
	scanner.closeAlternative()

	groupIndex := scanner.groupStack[len(scanner.groupStack)-1]
	scanner.groupStack = scanner.groupStack[:len(scanner.groupStack)-1]
	if groupIndex >= 0 {
		scanner.structure.groups[groupIndex].end = index + 1
	}
	scanner.pop()
	return true
}

// readEscape handles a `\` outside a character class and returns how many bytes it consumed.
//
// Two of them are backreferences and the rest are skipped by the shelf's own scanner, which already
// knows the widths of `\x`, `\u{...}`, `\p{...}` and `\q{...}` under each flag.
func (scanner *regexStructureScanner) readEscape(index int) (int, bool) {
	pattern := scanner.pattern
	if index+1 >= len(pattern) {
		return 0, false
	}

	// `\1` through `\9...`: a numeric backreference, or a legacy octal when no such group exists.
	// Which of the two it is cannot be decided here, because the group may be declared later in the
	// pattern; the total is known only after the scan, so the decision is deferred to resolution.
	if pattern[index+1] >= '1' && pattern[index+1] <= '9' {
		end := index + 1
		value := 0
		for end < len(pattern) && pattern[end] >= '0' && pattern[end] <= '9' {
			value = value*10 + int(pattern[end]-'0')
			end++
		}
		scanner.structure.backreferences = append(scanner.structure.backreferences, backreference{
			index: value,
			start: index,
			end:   end,
			path:  scanner.currentPath(),
		})
		return end - index, true
	}

	// `\k<name>`: a named backreference. Bare `\k` with no angle bracket is an identity escape
	// outside unicode mode, and the shelf's skip handles it.
	if pattern[index+1] == 'k' && index+2 < len(pattern) && pattern[index+2] == '<' {
		closing := strings.IndexByte(pattern[index+3:], '>')
		if closing >= 0 {
			end := index + 3 + closing + 1
			scanner.structure.backreferences = append(scanner.structure.backreferences, backreference{
				name:  pattern[index+3 : index+3+closing],
				start: index,
				end:   end,
				path:  scanner.currentPath(),
			})
			return end - index, true
		}
	}

	step, ok := regex_syntax.SkipPatternEscape(pattern, index, scanner.flags)
	if !ok {
		return 0, false
	}
	return step, true
}
