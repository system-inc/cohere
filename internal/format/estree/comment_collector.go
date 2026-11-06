package estree

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	ecmascripttext "github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

/*
 * Every comment in a source file, as ESTree comment nodes.
 *
 * Upstream is typescript-estree's convert-comments.js, which walks ts-api-utils' iterateComments: each
 * token's leading and trailing trivia, read through TypeScript's scanner. The trivia between tokens is
 * exactly where comments live, so this walks the same regions over typescript-go's tree: the gaps
 * between a node's children (trivia plus punctuation and keywords, which are tokens rather than
 * nodes), and the leading trivia of each leaf. A leaf's own text is never scanned, because a string,
 * a template or a regular expression can contain `//` without containing a comment.
 *
 * JSX text is skipped whole. It is a leaf that is all text, and `<p>// not a comment</p>` is text.
 */

// collectComments returns the file's comments in source order.
func collectComments(sourceFile *ast.SourceFile) []*Node {
	text := sourceFile.Text()
	collector := &commentCollector{text: text, seen: map[int]bool{}}
	collector.visit(sourceFile.AsNode())
	sort.Slice(collector.comments, func(left, right int) bool {
		return collector.comments[left].Range[0] < collector.comments[right].Range[0]
	})
	return collector.comments
}

type commentCollector struct {
	text     string
	comments []*Node
	seen     map[int]bool
}

func (collector *commentCollector) visit(node *ast.Node) {
	cursor := node.Pos()
	hasChildren := false
	node.ForEachChild(func(child *ast.Node) bool {
		hasChildren = true
		if child.Pos() >= cursor {
			collector.scanGap(cursor, child.Pos())
		}
		collector.visit(child)
		if child.End() > cursor {
			cursor = child.End()
		}
		return false
	})
	if !hasChildren {
		switch node.Kind {
		case ast.KindJsxText:
		case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindRegularExpressionLiteral,
			ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateHead, ast.KindTemplateMiddle, ast.KindTemplateTail:
			collector.scanTrivia(node.Pos(), node.End())
		default:
			// A childless node that is not a literal is trivia and tokens, like `{/* comment */}`, an
			// empty JSX expression whose comment sits between its braces.
			collector.scanGap(node.Pos(), node.End())
		}
		return
	}
	collector.scanGap(cursor, node.End())
}

// scanTrivia records the comments in the trivia that starts at pos, stopping at the first token.
func (collector *commentCollector) scanTrivia(position int, end int) {
	cursor := position
	for cursor < end {
		next, isTrivia := collector.skipOneTrivia(cursor, end)
		if !isTrivia {
			return
		}
		cursor = next
	}
}

// scanGap records the comments in text that holds only trivia and tokens that are not nodes:
// punctuation, keywords and operators. Quoted text is skipped as a guard, should a string ever sit in
// a gap, so its contents can never be read as a comment.
func (collector *commentCollector) scanGap(position int, end int) {
	text := collector.text
	cursor := position
	for cursor < end {
		next, isTrivia := collector.skipOneTrivia(cursor, end)
		if isTrivia {
			cursor = next
			continue
		}
		switch quote := text[cursor]; quote {
		case '"', '\'', '`':
			cursor++
			for cursor < end && text[cursor] != quote {
				if text[cursor] == '\\' {
					cursor++
				}
				cursor++
			}
			cursor++
		default:
			cursor++
		}
	}
}

// skipOneTrivia skips one whitespace character or one comment at pos, recording the comment, and
// reports whether there was trivia there at all.
func (collector *commentCollector) skipOneTrivia(position int, end int) (int, bool) {
	text := collector.text
	character := text[position]
	switch {
	case character == '/' && position+1 < end && text[position+1] == '/':
		start := position
		cursor := position + 2
		for cursor < end {
			if size := lineTerminatorSize(text, cursor); size > 0 {
				break
			}
			cursor++
		}
		collector.add("Line", start, cursor, text[start+2:cursor])
		return cursor, true
	case character == '/' && position+1 < end && text[position+1] == '*':
		start := position
		closing := strings.Index(text[position+2:end], "*/")
		if closing < 0 {
			collector.add("Block", start, end, text[start+2:end])
			return end, true
		}
		closed := position + 2 + closing + 2
		collector.add("Block", start, closed, text[start+2:closed-2])
		return closed, true
	}
	if size := whitespaceSize(text, position); size > 0 {
		return position + size, true
	}
	return position, false
}

func (collector *commentCollector) add(commentType string, start int, end int, value string) {
	if collector.seen[start] {
		return
	}
	collector.seen[start] = true
	collector.comments = append(collector.comments, New(commentType, start, end, []Property{{"value", String(value)}}))
}

// lineTerminatorSize is the byte length of the line terminator at pos, or 0: \n, \r, U+2028, U+2029.
func lineTerminatorSize(text string, position int) int {
	switch text[position] {
	case '\n', '\r':
		return 1
	case 0xE2:
		if strings.HasPrefix(text[position:], " ") || strings.HasPrefix(text[position:], " ") {
			return 3
		}
	}
	return 0
}

// whitespaceSize is the byte length of the JavaScript whitespace or line terminator at pos, or 0.
// JavaScript's \s: the ASCII spaces, `U+00A0`, `U+FEFF`, the Unicode Zs category, and line terminators.
func whitespaceSize(text string, position int) int {
	character := text[position]
	if character < utf8.RuneSelf {
		switch character {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			return 1
		}
		return 0
	}
	value, size := utf8.DecodeRuneInString(text[position:])
	if ecmascripttext.IsWhitespace(value) {
		return size
	}
	return 0
}
