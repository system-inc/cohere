package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strings"
)

// The pass's last step: a comment or a doc naming a file the pass removed would point at nothing, so a mention that
// is exactly the old file's name is rewritten to the file its content now lives in. A bare name is read only in a
// Go comment of the same directory, where it can mean nothing else; a path from the root is read in any Go comment,
// in the Markdown under docs/ and in the Markdown at the root, `README.md` and `CONTRIBUTING.md` among it. A mention
// whose referent cannot be decided is listed instead.

// mentionLimit is the house's line limit, a tab counted as four columns, which a rewritten line must keep.
const mentionLimit = 120

// referents maps each removed file's path from the root to the file a mention of it now means: the file it goes on
// as, renamed or merged into, else the one file its content went to, or "" when the split emptied it into several
// and which one a mention means is a guess. moves is every renames.tsv line and every continuation, from the root.
func referents(moves []fileMove, exists func(string) bool) map[string]string {
	destinations := map[string][]string{}
	for _, move := range moves {
		if move.to != move.from && !contains(destinations[move.from], move.to) {
			destinations[move.from] = append(destinations[move.from], move.to)
		}
	}
	result := map[string]string{}
	for from, list := range destinations {
		if exists(from) {
			continue
		}
		result[from] = ""
		if len(list) == 1 {
			result[from] = list[0]
		}
	}
	for _, move := range moves {
		if move.continues && !exists(move.from) {
			result[move.from] = move.to
		}
	}
	return result
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// mentionOutcome is what rewriteMentions did: the new content of each file it changed, and a line per mention it
// rewrote or left.
type mentionOutcome struct {
	changes   map[string][]byte
	rewritten []string
	left      []string
}

// inMentionScope is whether a file of the tree, by its path from the root, is read for mentions.
func inMentionScope(relative string) bool {
	switch {
	case strings.HasSuffix(relative, ".go"):
		return true
	case strings.HasSuffix(relative, ".md") && !strings.Contains(relative, "/"):
		return true
	default:
		return strings.HasPrefix(relative, "docs/") && strings.HasSuffix(relative, ".md")
	}
}

// rewriteMentions rewrites the mentions of removed files in tree, every file in scope by its path from the root
// with its content after the pass. A Go file is rewritten only where the mention sits in a comment, only when its
// declarations print the same afterwards and gofmt leaves it as it is, and a line that would pass mentionLimit is
// left.
func rewriteMentions(tree map[string][]byte, removed map[string]string) mentionOutcome {
	outcome := mentionOutcome{changes: map[string][]byte{}}
	paths := make([]string, 0, len(tree))
	for relative := range tree {
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	olds := make([]string, 0, len(removed))
	for old := range removed {
		olds = append(olds, old)
	}
	sort.Strings(olds)
	for _, relative := range paths {
		content := tree[relative]
		isGo := strings.HasSuffix(relative, ".go")
		directory := path.Dir(relative)
		var spans []byteRange
		if isGo {
			fileSet := token.NewFileSet()
			syntax, parseError := parser.ParseFile(fileSet, relative, content, parser.ParseComments)
			if parseError != nil {
				continue
			}
			tokenFile := fileSet.File(syntax.Pos())
			for _, group := range syntax.Comments {
				for _, comment := range group.List {
					spans = append(
						spans,
						byteRange{start: tokenFile.Offset(comment.Pos()), end: tokenFile.Offset(comment.End())},
					)
				}
			}
		} else {
			spans = []byteRange{{start: 0, end: len(content)}}
		}
		var edits []edit
		for _, old := range olds {
			// The bare name only from a Go file of the same directory, the path from the root from anywhere.
			needles := []string{old}
			if isGo && path.Dir(old) == directory {
				needles = append(needles, path.Base(old))
			}
			for _, needle := range needles {
				for _, span := range spans {
					for _, offset := range exactMentions(content, span, needle) {
						where := fmt.Sprintf(
							"%s:%d: %s",
							relative,
							bytes.Count(content[:offset], []byte("\n"))+1,
							needle,
						)
						referent := removed[old]
						after := content[offset+len(needle):]
						switch {
						case len(after) >= 2 && after[0] == ':' && after[1] >= '0' && after[1] <= '9':
							outcome.left = append(
								outcome.left,
								where+": carries a line number, which the move made wrong",
							)
						case referent == "":
							outcome.left = append(outcome.left, where+": its content went to several files")
						default:
							replacement := referent
							if needle != old {
								replacement = path.Base(referent)
							}
							if isGo && lineWidthAfter(content, offset, len(needle), replacement) > mentionLimit {
								outcome.left = append(outcome.left, fmt.Sprintf("%s: the line would pass %d columns",
									where, mentionLimit))
								continue
							}
							edits = append(edits, edit{start: offset, end: offset + len(needle), text: replacement})
							outcome.rewritten = append(outcome.rewritten, where+" -> "+replacement)
						}
					}
				}
			}
		}
		if len(edits) == 0 {
			continue
		}
		rewritten := applyEdits(content, edits)
		if problem := mentionProblem(relative, content, rewritten, isGo); problem != "" {
			// Nothing of the file is rewritten, and each of its mentions is listed with why.
			kept := outcome.rewritten[:0]
			for _, line := range outcome.rewritten {
				if strings.HasPrefix(line, relative+":") {
					outcome.left = append(outcome.left, strings.SplitN(line, " -> ", 2)[0]+": "+problem)
				} else {
					kept = append(kept, line)
				}
			}
			outcome.rewritten = kept
			continue
		}
		outcome.changes[relative] = rewritten
	}
	return outcome
}

// exactMentions is the offset of each place in content's span that spells needle as a whole name: not inside a
// longer name or path, so walk.go in internal/program/walk.go or in graph_walk.go is no mention of it.
func exactMentions(content []byte, span byteRange, needle string) []int {
	var offsets []int
	text := content[span.start:span.end]
	for from := 0; ; {
		index := bytes.Index(text[from:], []byte(needle))
		if index < 0 {
			return offsets
		}
		start := from + index
		end := start + len(needle)
		from = end
		if start > 0 && isNameByte(text[start-1], true) {
			continue
		}
		if end < len(text) && (isNameByte(text[end], false) ||
			(text[end] == '.' && end+1 < len(text) && isNameByte(text[end+1], false))) {
			continue
		}
		offsets = append(offsets, span.start+start)
	}
}

// isNameByte is whether character continues a file name or path: a letter, a digit, an underscore, a slash or a
// dash, and before a name a dot too, as in ../walk.go.
func isNameByte(character byte, before bool) bool {
	switch {
	case character >= 'a' &&
		character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9':
		return true
	case character == '_' || character == '/' || character == '-':
		return true
	default:
		return before && character == '.'
	}
}

// mentionProblem says why a file's rewritten content cannot replace it, or is empty: a Go file must keep its
// declarations, stay as gofmt writes it, and gain no line past mentionLimit.
func mentionProblem(relative string, content []byte, rewritten []byte, isGo bool) string {
	if !isGo {
		return ""
	}
	before, beforeError := declarationMultiset(map[string][]byte{relative: content})
	after, afterError := declarationMultiset(map[string][]byte{relative: rewritten})
	if beforeError != nil || afterError != nil {
		return "the file does not parse"
	}
	if difference := multisetDifference(before, after); difference != "" {
		return "the rewrite would change a declaration"
	}
	if formatted, formatError := format.Source(rewritten); formatError != nil || !bytes.Equal(formatted, rewritten) {
		return "gofmt would change the rewritten file"
	}
	oldLines := strings.Split(string(content), "\n")
	for number, line := range strings.Split(string(rewritten), "\n") {
		if columns(line) > mentionLimit && (number >= len(oldLines) || oldLines[number] != line) {
			return fmt.Sprintf("line %d would pass %d columns", number+1, mentionLimit)
		}
	}
	return ""
}

// columns is a line's width with a tab counted as four.
func columns(line string) int {
	return len([]rune(line)) + 3*strings.Count(line, "\t")
}

// lineWidthAfter is the width of the line holding offset once the length bytes there are replacement.
func lineWidthAfter(content []byte, offset int, length int, replacement string) int {
	start := bytes.LastIndexByte(content[:offset], '\n') + 1
	end := len(content)
	if index := bytes.IndexByte(content[offset:], '\n'); index >= 0 {
		end = offset + index
	}
	line := string(content[start:offset]) + replacement + string(content[offset+length:end])
	return columns(line)
}
