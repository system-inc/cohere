// The CSS selector parser: a single left-to-right pass that turns a selector string into a tree of
// lists, complex selectors, compound selectors, combinators, functions, and the simple selectors
// they hold.
//
// Ported from `src/selector-parser.ts` at the v4.3.3 tag, read at the tag rather than from the
// minified bundle, so a disagreement with the engine is a finding rather than version skew.
//
// # The shape of the tree
//
// A selector list is a `list` node holding one item per comma. An item with a combinator in it is a
// `complex` node, alternating compounds (or lone simple selectors) with `combinator` nodes. Simple
// selectors written back to back are a `compound`. A lone simple selector is never wrapped, so
// `.foo` is a single `selector` node rather than a compound of one.
//
// Only four functions have their arguments parsed as selectors: `:not`, `:where`, `:has` and `:is`.
// Every other function's arguments become one opaque `value` node, except that `:nth-child` and
// `:nth-last-child` split at an `of ` clause into a value for the `An+B` part and a parsed selector
// list for the rest, so a `&` inside the clause can be found and substituted.
//
// # Deliberate fidelity to JavaScript semantics
//
// Several behaviors fall out of how the upstream loop is written rather than out of stated intent.
// They are ported as they are, each is called out at its branch, and each was confirmed by running
// upstream's own source:
//
//   - Whitespace in front of a `)` is a descendant combinator. The rule that drops insignificant
//     whitespace looks only for the end of input and a following comma, so `:is(.a )` holds a
//     complex selector ending in a ` ` combinator.
//   - A function other than the four above, left unclosed, gets an empty value, and what would
//     have been its argument is parsed as selectors beside it. `:hover(abc` reprints as
//     `:hover()abc`.
//   - An unclosed `[` is the one-character selector `[`, and parsing resumes after it.
//   - A trailing `\` yields the literal text `\undefined`, because upstream concatenates
//     `input[i + 1]`, which is `undefined` one past the end of a JavaScript string.
//   - A `)` with no open function resets the parser to the top level and forgets any selector list
//     in progress, so `.a, .b) .c` is a complex selector whose first part is the list `.a, .b`.
//   - An empty list item, as in `.foo,` or `.foo,,.bar`, is an empty compound, and reprints as
//     nothing, so the invalid list stays invalid rather than being silently repaired.
//
// A port that "fixed" any of these would be wrong in the direction of a plausible tree, which is
// the failure mode that is expensive to notice.
package tailwind

import (
	"strings"

	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// SelectorNodeKind distinguishes the seven node shapes the parser produces.
type SelectorNodeKind string

const (
	// SelectorNodeKindList is a comma-separated selector list. Nodes holds one node per item.
	SelectorNodeKindList SelectorNodeKind = "list"
	// SelectorNodeKindCombinator is a combinator between two parts of a complex selector. Value is
	// ` `, `>`, `+` or `~` for anything well formed, and the trimmed run verbatim otherwise, so
	// `.a > + .b` yields the single combinator `> +`.
	SelectorNodeKindCombinator SelectorNodeKind = "combinator"
	// SelectorNodeKindComplex is a selector with at least one combinator in it.
	SelectorNodeKindComplex SelectorNodeKind = "complex"
	// SelectorNodeKindCompound is two or more simple selectors written back to back, and also the
	// empty item of an invalid list such as `.foo,`.
	SelectorNodeKindCompound SelectorNodeKind = "compound"
	// SelectorNodeKindSelector is a simple selector: a class, an id, a pseudo-class or
	// pseudo-element, an attribute selector, a type selector, `&`, or `*`.
	SelectorNodeKindSelector SelectorNodeKind = "selector"
	// SelectorNodeKindFunction is a functional pseudo-class. Value holds the name with its colon,
	// such as `:not`, and is empty for a bare `(`.
	SelectorNodeKindFunction SelectorNodeKind = "function"
	// SelectorNodeKindValue is a function argument held verbatim rather than parsed as a selector.
	SelectorNodeKindValue SelectorNodeKind = "value"
)

// SelectorNode is one node of the parsed selector tree.
//
// One struct rather than an interface with seven implementations, matching the upstream tagged
// union and ValueNode beside it. Callers switch on Kind. Value is meaningful for a combinator, a
// selector, a value, and a function's name. Nodes is meaningful for a list, a complex, a compound,
// and a function's arguments, and is a non-nil slice for exactly those four kinds, even when empty,
// matching upstream's `nodes: []`, so an empty list item is distinguishable from a leaf.
//
// No JSON tags, unlike ValueNode: nothing encodes this tree, and a flat struct cannot reproduce
// upstream's per-kind key set anyway, since a list has no `value` key at all while a function with
// an empty name has an empty one.
type SelectorNode struct {
	Kind SelectorNodeKind
	// Value is the combinator, the simple selector's text, the opaque argument, or the function
	// name.
	Value string
	// Nodes are the children of a list, complex, compound or function, and nil for every other
	// kind.
	Nodes []SelectorNode
}

// The characters with a branch of their own, as the upstream constant set.
const (
	selectorAmpersand    = '&'
	selectorAsterisk     = '*'
	selectorBackslash    = '\\'
	selectorCloseBracket = ']'
	selectorCloseParen   = ')'
	selectorColon        = ':'
	selectorComma        = ','
	selectorDot          = '.'
	selectorDoubleQuote  = '"'
	selectorGreaterThan  = '>'
	selectorHash         = '#'
	selectorNewline      = '\n'
	selectorOpenBracket  = '['
	selectorOpenParen    = '('
	selectorPlus         = '+'
	selectorSingleQuote  = '\''
	selectorSpace        = ' '
	selectorTab          = '\t'
	selectorTilde        = '~'
)

// isSelectorCombinatorCharacter reports whether the byte joins a combinator run: whitespace and the
// three explicit combinators.
//
// Byte-wise rather than rune-wise, which is safe because every member is ASCII and UTF-8
// continuation bytes are all >= 0x80, so no multi-byte character can be mistaken for one. The same
// argument covers every other byte comparison in this file.
func isSelectorCombinatorCharacter(character byte) bool {
	switch character {
	case selectorGreaterThan, selectorNewline, selectorSpace, selectorPlus, selectorTab, selectorTilde:
		return true
	}
	return false
}

// firstSelectorCharacter returns the first byte of a selector node's value, and false for any other
// kind or for an empty value. Upstream's predicates read `charCodeAt(0)`, which is `NaN` on an empty
// string and so matches no character.
func firstSelectorCharacter(node SelectorNode) (byte, bool) {
	if node.Kind != SelectorNodeKindSelector || node.Value == "" {
		return 0, false
	}
	return node.Value[0], true
}

// IsUniversalSelector reports whether the node is `*`, upstream's `isUniversalSelector`.
func IsUniversalSelector(node SelectorNode) bool {
	first, ok := firstSelectorCharacter(node)
	return ok && first == selectorAsterisk
}

// IsNestingSelector reports whether the node is `&`, upstream's `isNestingSelector`.
func IsNestingSelector(node SelectorNode) bool {
	first, ok := firstSelectorCharacter(node)
	return ok && first == selectorAmpersand
}

// IsClassSelector reports whether the node is a class selector, upstream's `isClassSelector`.
func IsClassSelector(node SelectorNode) bool {
	first, ok := firstSelectorCharacter(node)
	return ok && first == selectorDot
}

// IsIdSelector reports whether the node is an id selector, upstream's `isIdSelector`.
func IsIdSelector(node SelectorNode) bool {
	first, ok := firstSelectorCharacter(node)
	return ok && first == selectorHash
}

// IsPseudoSelector reports whether the node is a pseudo-class or pseudo-element without arguments,
// upstream's `isPseudoSelector`. A functional pseudo-class is a function node, not a selector, so
// `:not(…)` is not one.
func IsPseudoSelector(node SelectorNode) bool {
	first, ok := firstSelectorCharacter(node)
	return ok && first == selectorColon
}

// IsAttributeSelector reports whether the node is an attribute selector, upstream's
// `isAttributeSelector`.
func IsAttributeSelector(node SelectorNode) bool {
	first, ok := firstSelectorCharacter(node)
	return ok && first == selectorOpenBracket
}

// IsTypeSelector reports whether the node is a type selector, upstream's `isTypeSelector`.
//
// Like upstream it does not check that the text is a valid type selector, only that the node is a
// selector of none of the other kinds. A selector with an empty value is therefore a type selector:
// upstream's `charCodeAt(0)` is `NaN` there, which falls to the default branch. The parser never
// produces an empty selector, so only a hand-built node can reach that case.
func IsTypeSelector(node SelectorNode) bool {
	if node.Kind != SelectorNodeKindSelector {
		return false
	}
	if node.Value == "" {
		return true
	}
	switch node.Value[0] {
	case selectorAsterisk, selectorAmpersand, selectorDot, selectorHash, selectorColon, selectorOpenBracket:
		return false
	}
	return true
}

// CloneSelectorNode deep-copies a node, upstream's `cloneAstNode`.
//
// The copy carries only the fields its kind uses, as upstream's does: a leaf keeps Value and drops
// Nodes, a list, complex or compound keeps Nodes and drops Value. A Go struct copy alone would share
// the Nodes backing arrays, so an edit to the clone's children would show through in the original.
// Upstream throws on a kind it does not know; a SelectorNodeKind outside the seven is returned as a
// copy of its Kind alone, since nothing in this package can construct one.
func CloneSelectorNode(node SelectorNode) SelectorNode {
	switch node.Kind {
	case SelectorNodeKindCombinator, SelectorNodeKindSelector, SelectorNodeKindValue:
		return SelectorNode{Kind: node.Kind, Value: node.Value}
	case SelectorNodeKindComplex, SelectorNodeKindCompound, SelectorNodeKindList:
		return SelectorNode{Kind: node.Kind, Nodes: cloneSelectorNodes(node.Nodes)}
	case SelectorNodeKindFunction:
		return SelectorNode{Kind: node.Kind, Value: node.Value, Nodes: cloneSelectorNodes(node.Nodes)}
	}
	return SelectorNode{Kind: node.Kind}
}

// cloneSelectorNodes is upstream's `node.nodes.map(cloneAstNode)`, and keeps the slice non-nil.
func cloneSelectorNodes(nodes []SelectorNode) []SelectorNode {
	cloned := make([]SelectorNode, 0, len(nodes))
	for _, node := range nodes {
		cloned = append(cloned, CloneSelectorNode(node))
	}
	return cloned
}

// SelectorToCss reprints a node tree, upstream's `toCss`. Upstream's `minify` argument is optional
// and defaults to false.
//
// Printing normalizes rather than restoring the input: a list item is joined with `, `, or `,` when
// minified, and an explicit combinator gets one space on each side, or none when minified. The
// descendant combinator is always a single space, because without it two compounds would run
// together into one.
func SelectorToCss(nodes []SelectorNode, minify bool) string {
	var builder strings.Builder
	writeSelectorCss(&builder, nodes, minify)
	return builder.String()
}

func writeSelectorCss(builder *strings.Builder, nodes []SelectorNode, minify bool) {
	for _, node := range nodes {
		switch node.Kind {
		case SelectorNodeKindSelector, SelectorNodeKindValue:
			builder.WriteString(node.Value)
		case SelectorNodeKindCombinator:
			if minify || node.Value == " " {
				builder.WriteString(node.Value)
			} else {
				builder.WriteString(" " + node.Value + " ")
			}
		case SelectorNodeKindFunction:
			builder.WriteString(node.Value)
			builder.WriteByte('(')
			writeSelectorCss(builder, node.Nodes, minify)
			builder.WriteByte(')')
		case SelectorNodeKindComplex, SelectorNodeKindCompound:
			writeSelectorCss(builder, node.Nodes, minify)
		case SelectorNodeKindList:
			separator := ", "
			if minify {
				separator = ","
			}
			for index, item := range node.Nodes {
				if index > 0 {
					builder.WriteString(separator)
				}
				writeSelectorCss(builder, []SelectorNode{item}, minify)
			}
		}
	}
}

// selectorTreeNode is the parser's working node. Upstream builds its tree by mutating shared
// references: it keeps the open selector list, the innermost open function's arguments, and the
// last compound as live handles and pushes into them. Pointers reproduce that directly, and the
// finished tree is copied into SelectorNode values once at the end.
type selectorTreeNode struct {
	kind  SelectorNodeKind
	value string
	nodes []*selectorTreeNode
}

// ParseSelector parses a selector string into a node tree, upstream's `parse`.
//
// Total: every input parses, including unbalanced and truncated ones, because upstream never
// signals an error and selectors from third-party CSS are not guaranteed valid.
func ParseSelector(input string) []SelectorNode {
	return toSelectorNodes(parseSelectorTree(input))
}

// toSelectorNodes copies the working tree into values, giving the four container kinds a non-nil
// Nodes slice even when they are empty, as SelectorNode documents.
func toSelectorNodes(nodes []*selectorTreeNode) []SelectorNode {
	converted := make([]SelectorNode, 0, len(nodes))
	for _, node := range nodes {
		selectorNode := SelectorNode{Kind: node.kind, Value: node.value}
		switch node.kind {
		case SelectorNodeKindList, SelectorNodeKindComplex, SelectorNodeKindCompound, SelectorNodeKindFunction:
			selectorNode.Nodes = toSelectorNodes(node.nodes)
		}
		converted = append(converted, selectorNode)
	}
	return converted
}

// selectorParseContext is what opening a selector function saves and closing it restores.
type selectorParseContext struct {
	target             *[]*selectorTreeNode
	currentList        *selectorTreeNode
	containsCombinator bool
}

// parseSelectorTree is the body of upstream's `parse`. It is separate from ParseSelector because
// the `:nth-child(… of …)` branch recurses into it and splices the working nodes in.
func parseSelectorTree(input string) []*selectorTreeNode {
	// Upstream normalizes CRLF first, so a `\r\n` is one combinator character rather than two. A
	// lone `\r` is untouched and falls through to the default branch as an ordinary character.
	input = strings.ReplaceAll(input, "\r\n", "\n")

	ast := []*selectorTreeNode{}

	// target is the slice new nodes go into: the top level, an open function's arguments, or a
	// fresh slice collecting the current item of a selector list. A pointer to the slice header
	// rather than the slice, because upstream's `target` is an array reference that every branch
	// pushes through, and a Go slice copied into a local would stop seeing the appends.
	target := &ast

	// containsCombinator records whether the nodes collected in target include a combinator, which
	// decides whether they are wrapped as a complex or a compound.
	containsCombinator := false

	var contextStack []selectorParseContext

	// currentList is the selector list being filled at this level, held so each item can be pushed
	// into it while target collects the next one.
	var currentList *selectorTreeNode

	var buffer strings.Builder

	// current wraps a run of nodes as one: the node itself when there is exactly one, otherwise a
	// complex or a compound, upstream's `current`. Upstream's default argument is target, read at
	// the call.
	current := func(nodes []*selectorTreeNode) *selectorTreeNode {
		if len(nodes) == 1 {
			return nodes[0]
		}
		if containsCombinator {
			return &selectorTreeNode{kind: SelectorNodeKindComplex, nodes: nodes}
		}
		return &selectorTreeNode{kind: SelectorNodeKindCompound, nodes: nodes}
	}

	// appendNode is upstream's `append`. A node written directly after another joins it in a
	// compound: an existing compound grows, and any other node except a list or a combinator is
	// wrapped together with the new one. After a list or a combinator, or at the start, the node
	// stands alone.
	appendNode := func(node *selectorTreeNode) {
		if length := len(*target); length > 0 {
			existing := (*target)[length-1]
			if existing.kind == SelectorNodeKindCompound {
				existing.nodes = append(existing.nodes, node)
				return
			}
			if existing.kind != SelectorNodeKindList && existing.kind != SelectorNodeKindCombinator {
				(*target)[length-1] = &selectorTreeNode{
					kind:  SelectorNodeKindCompound,
					nodes: []*selectorTreeNode{existing, node},
				}
				return
			}
		}
		*target = append(*target, node)
	}

	// flushBuffer emits the pending buffer as a simple selector, if non-empty.
	flushBuffer := func() {
		if buffer.Len() == 0 {
			return
		}
		appendNode(&selectorTreeNode{kind: SelectorNodeKindSelector, value: buffer.String()})
		buffer.Reset()
	}

	for index := 0; index < len(input); index++ {
		character := input[index]

		switch character {
		// A comma ends one item of a selector list and starts the next.
		case selectorComma:
			flushBuffer()

			// Whitespace after the comma is skipped. Whitespace before it never reaches here: the
			// combinator branch drops a whitespace run that a comma follows.
			for ; index+1 < len(input); index++ {
				peek := input[index+1]
				if peek != selectorNewline && peek != selectorSpace && peek != selectorTab {
					break
				}
			}

			if currentList != nil {
				// A list is open at this level: the collected item joins it, and collection starts
				// over in a fresh slice.
				currentList.nodes = append(currentList.nodes, current(*target))
				target = &[]*selectorTreeNode{}
				containsCombinator = false
			} else {
				// The first comma at this level: everything collected so far becomes the list's
				// first item, the list takes its place in target, and the next item is collected
				// in a fresh slice. With nothing collected, as in `,.foo`, the first item is an
				// empty compound.
				nodes := *target
				*target = nil
				list := &selectorTreeNode{kind: SelectorNodeKindList, nodes: []*selectorTreeNode{current(nodes)}}
				*target = append(*target, list)
				currentList = list
				target = &[]*selectorTreeNode{}
				containsCombinator = false
			}

		// A run of whitespace and combinator characters is one combinator.
		case selectorGreaterThan, selectorNewline, selectorSpace, selectorPlus, selectorTab, selectorTilde:
			flushBuffer()

			start := index
			end := index + 1
			for ; end < len(input); end++ {
				if !isSelectorCombinatorCharacter(input[end]) {
					break
				}
			}
			index = end - 1

			// Upstream trims with JavaScript's `trim`, which is text.TrimWhitespace.
			value := text.TrimWhitespace(input[start:end])

			// Whitespace alone is insignificant at the start, at the end of input, and in front of
			// a comma. Not in front of a `)`, which is why `:is(.a )` keeps a trailing descendant
			// combinator. An explicit combinator is kept everywhere, even at the start.
			if value == "" && (len(*target) == 0 || end >= len(input) || input[end] == selectorComma) {
				break
			}

			if value == "" {
				value = " "
			}
			*target = append(*target, &selectorTreeNode{kind: SelectorNodeKindCombinator, value: value})
			containsCombinator = true

		// A `(` opens a function whose name is whatever the buffer held, which is empty for a bare
		// `(`.
		case selectorOpenParen:
			node := &selectorTreeNode{kind: SelectorNodeKindFunction, value: buffer.String(), nodes: []*selectorTreeNode{}}
			buffer.Reset()

			// Only these four take a selector argument. Every other function's arguments are taken
			// whole as one value, matching parens but nothing else, so a `)` inside a quoted string
			// in the argument still closes the function.
			if node.value != ":not" && node.value != ":where" && node.value != ":has" && node.value != ":is" {
				start := index + 1
				nesting := 0

				for scan := index + 1; scan < len(input); scan++ {
					peek := input[scan]
					if peek == selectorOpenParen {
						nesting++
						continue
					}
					if peek == selectorCloseParen {
						if nesting == 0 {
							index = scan
							break
						}
						nesting--
					}
				}
				end := index

				// An unclosed function leaves index at the `(`, so start is past end. JavaScript's
				// `slice` returns the empty string for that rather than failing, so the function
				// gets an empty value and parsing resumes right after the `(`: what would have been
				// the argument is parsed as selectors beside the function.
				contents := ""
				if start <= end {
					contents = input[start:end]
				}

				// `:nth-child(…)` and `:nth-last-child(…)` can carry an `of <selector-list>`
				// clause, which is parsed so a `&` inside it can be substituted. The `An+B` part
				// before it is not a selector and stays an opaque value, ending with the `of `
				// itself. The search is for the first `of ` anywhere in the argument, followed by a
				// space specifically, so `of` followed by a tab or newline is not a clause.
				if node.value == ":nth-child" || node.value == ":nth-last-child" {
					if clause := strings.Index(contents, "of "); clause != -1 {
						node.nodes = append(node.nodes, &selectorTreeNode{kind: SelectorNodeKindValue, value: contents[:clause+3]})
						node.nodes = append(node.nodes, parseSelectorTree(contents[clause+3:])...)
						index = end
						appendNode(node)
						break
					}
				}

				node.nodes = append(node.nodes, &selectorTreeNode{kind: SelectorNodeKindValue, value: contents})
				index = end
				appendNode(node)
				break
			}

			// A selector function: its arguments are parsed as a selector of their own, with the
			// enclosing state saved and restored at the matching `)`.
			appendNode(node)
			contextStack = append(contextStack, selectorParseContext{
				target:             target,
				currentList:        currentList,
				containsCombinator: containsCombinator,
			})
			target = &node.nodes
			containsCombinator = false
			currentList = nil

		// A `)` closes the innermost selector function. A non-selector function's `)` never reaches
		// here, having been consumed with its argument.
		case selectorCloseParen:
			flushBuffer()

			if currentList != nil {
				currentList.nodes = append(currentList.nodes, current(*target))
			} else if containsCombinator {
				// The arguments held a combinator, so they are wrapped as one complex selector in
				// place.
				nodes := *target
				*target = []*selectorTreeNode{{kind: SelectorNodeKindComplex, nodes: nodes}}
			}

			// With nothing open, upstream pops `undefined` and falls back to the top level with no
			// list and no combinator. A list in progress at the top level is therefore forgotten
			// rather than continued: it stays in the tree, and what follows is appended beside it.
			if length := len(contextStack); length > 0 {
				context := contextStack[length-1]
				contextStack = contextStack[:length-1]
				target = context.target
				currentList = context.currentList
				containsCombinator = context.containsCombinator
			} else {
				target = &ast
				currentList = nil
				containsCombinator = false
			}

		// A `.`, `:` or `#` starts a new simple selector, splitting a compound such as `.foo.bar`.
		// The exception is the second colon of a pseudo-element, which extends the buffer so
		// `::before` stays one selector.
		case selectorDot, selectorColon, selectorHash:
			if character == selectorColon && buffer.String() == ":" {
				buffer.WriteByte(character)
				break
			}
			flushBuffer()
			buffer.WriteByte(character)

		// An attribute selector is taken whole, from the `[` to the matching `]`. Nothing inside it
		// is examined but brackets, so a `]` inside a quoted attribute value ends it early. Left
		// unclosed, index stays at the `[`, which becomes a selector of its own, and parsing
		// resumes right after it.
		case selectorOpenBracket:
			flushBuffer()

			start := index
			nesting := 0

			for scan := index + 1; scan < len(input); scan++ {
				peek := input[scan]
				if peek == selectorOpenBracket {
					nesting++
					continue
				}
				if peek == selectorCloseBracket {
					if nesting == 0 {
						index = scan
						break
					}
					nesting--
				}
			}

			appendNode(&selectorTreeNode{kind: SelectorNodeKindSelector, value: input[start : index+1]})

		// A quoted string is taken whole into the buffer, closing only on a matching quote, so a `'`
		// inside a `"` string does not end it.
		case selectorSingleQuote, selectorDoubleQuote:
			start := index
			for scan := index + 1; scan < len(input); scan++ {
				peek := input[scan]
				if peek == selectorBackslash {
					// The next character is escaped and cannot close the string.
					scan++
				} else if peek == character {
					index = scan
					break
				}
			}
			// An unterminated string leaves index untouched, so only the quote character itself is
			// consumed and parsing resumes at the next character. `"a b` is therefore a complex
			// selector of `"a` and `b`, not one string.
			buffer.WriteString(input[start : index+1])

		// The nesting selector `&` and the universal selector `*` are always selectors of their
		// own, even directly against other text.
		case selectorAmpersand, selectorAsterisk:
			flushBuffer()
			appendNode(&selectorTreeNode{kind: SelectorNodeKindSelector, value: string(character)})

		// A `\` escapes the next character: both are consumed into the buffer, and neither is
		// examined.
		case selectorBackslash:
			if index+1 < len(input) {
				// One byte, not one rune, and the two are indistinguishable here. Upstream indexes by
				// UTF-16 code unit, so for a character outside the BMP it consumes only the high
				// surrogate and the low one reaches the default branch, which appends it. Byte-wise,
				// the continuation bytes reach the default branch the same way. Either way the
				// escaped character arrives in the buffer whole.
				buffer.WriteString(input[index : index+2])
			} else {
				// A trailing backslash reads one past the end. In JavaScript that is `undefined`,
				// and `'\\' + undefined` is the string `\undefined`. Reproduced literally, absurd as
				// it reads.
				buffer.WriteString(`\undefined`)
			}
			index++

		default:
			buffer.WriteByte(character)
		}
	}

	// The remainder lands in whatever target is current, unlike ParseValue, whose remainder always
	// lands at the top level. An unclosed `:is(.a` therefore holds `.a`.
	flushBuffer()

	if currentList != nil {
		currentList.nodes = append(currentList.nodes, current(*target))
	} else if containsCombinator {
		nodes := *target
		*target = []*selectorTreeNode{{kind: SelectorNodeKindComplex, nodes: nodes}}
	}

	return ast
}
