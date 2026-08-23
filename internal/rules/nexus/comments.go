package nexus

import (
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/scanner"
)

// Three rules judge comments rather than nodes, and a comment is not a node: it is trivia the
// parser skips, recorded only as a range in the source text. So they cannot register a listener for
// it, and each would otherwise write its own scan. This is that scan, written once.

// Comment is one comment in a file: where it is, what kind it is, and its text.
type Comment struct {
	Range core.TextRange

	// IsBlock distinguishes a slash-star comment from a double-slash one. JSDoc is a block comment
	// whose text opens with an asterisk, so the two rules that care about JSDoc need this to tell
	// the shapes apart.
	IsBlock bool

	// Text is the full source text of the comment, delimiters included. Rules that want the inner
	// content strip the delimiters themselves, because what counts as content differs by rule: the
	// JSDoc rule wants the asterisk-prefixed lines, the line-length rule wants the whole line.
	Text string

	// StartLine, StartColumn, and EndLine are zero-based positions. The run-detecting rule needs
	// them to answer two questions no range can: whether two comments sit on adjacent lines, and
	// whether they start at the same column, which is what separates a stacked passage from a
	// trailing comment that happens to follow another.
	StartLine   int
	StartColumn int
	EndLine     int
}

// allComments returns every comment in a file, in source order, without duplicates.
//
// The parser skips trivia, so there is no list to read; the comments have to be rescanned out of
// the source text. The scan walks from position zero and from the start of every node, because the
// scanner's iterator yields the run of comments beginning at a position and stops at the first
// token. Positions repeat across a walk (a node and its first child usually start at the same
// place), so the results are deduplicated by range rather than assumed distinct.
func allComments(sourceFile *ast.SourceFile) []Comment {
	if sourceFile == nil {
		return nil
	}
	text := sourceFile.Text()

	var factory ast.NodeFactory
	seenRanges := make(map[core.TextRange]bool)
	var comments []Comment

	record := func(commentRange ast.CommentRange) {
		if seenRanges[commentRange.TextRange] {
			return
		}
		seenRanges[commentRange.TextRange] = true

		start, end := commentRange.Pos(), commentRange.End()
		if start < 0 || end > len(text) || start >= end {
			return
		}
		startLine, startColumn := scanner.GetLineAndCharacterOfPosition(sourceFile, start)
		endLine, _ := scanner.GetLineAndCharacterOfPosition(sourceFile, end)

		comments = append(comments, Comment{
			Range:       commentRange.TextRange,
			IsBlock:     commentRange.Kind == ast.KindMultiLineCommentTrivia,
			Text:        text[start:end],
			StartLine:   startLine,
			StartColumn: startColumn,
			EndLine:     endLine,
		})
	}

	// Both directions are needed, and the pair is the whole reason this helper has a coverage test.
	// A leading scan sees a comment that begins a run of trivia; a comment sitting after code on the
	// same line is trailing trivia and is invisible to it. That gap costs nothing at parse time and
	// everything at review time, because a rule silently reports nothing for the comments it never
	// received, and the fixtures pass.
	collectAt := func(position int) {
		if position < 0 || position > len(text) {
			return
		}
		for commentRange := range scanner.GetLeadingCommentRanges(&factory, text, position) {
			record(commentRange)
		}
		for commentRange := range scanner.GetTrailingCommentRanges(&factory, text, position) {
			record(commentRange)
		}
	}

	// Position zero catches the file's opening comments, which precede every node.
	collectAt(0)

	var visit func(node *ast.Node)
	visit = func(node *ast.Node) {
		if node == nil {
			return
		}
		// A node's Pos() is where its leading trivia begins, not where its first token does, which
		// is exactly the position the comment scanner wants. End() is where a trailing comment
		// would sit, and the two positions find different comments.
		collectAt(node.Pos())
		collectAt(node.End())
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile.AsNode())

	// The end-of-file token carries any comment that trails the last statement. Without this a
	// comment after the final line is invisible, which is the failure a rule would never notice
	// because nothing reports a comment it did not see.
	collectAt(sourceFile.EndOfFileToken.Pos())

	sortCommentsByPosition(comments)
	return comments
}

// sortCommentsByPosition puts comments in source order.
//
// The walk reaches nodes in tree order, which is close to source order but not equal to it, and a
// rule reporting findings out of order is a rule whose fixture assertions cannot be written down.
func sortCommentsByPosition(comments []Comment) {
	for outerIndex := 1; outerIndex < len(comments); outerIndex++ {
		current := comments[outerIndex]
		innerIndex := outerIndex - 1
		for innerIndex >= 0 && comments[innerIndex].Range.Pos() > current.Range.Pos() {
			comments[innerIndex+1] = comments[innerIndex]
			innerIndex--
		}
		comments[innerIndex+1] = current
	}
}

// isJsDoc reports whether a block comment is JSDoc: slash-star-star rather than slash-star.
func (c Comment) isJsDoc() bool {
	return c.IsBlock && strings.HasPrefix(c.Text, "/**")
}

// contentLines returns a block comment's inner lines with the delimiters and the leading asterisk
// of each line removed, dropping lines that hold nothing else.
//
// This is what "the comment says one thing" means for the JSDoc rules: the asterisks are furniture,
// so a comment that is three lines of furniture around one line of prose is a one-line comment.
func (c Comment) contentLines() []string {
	inner := c.Text
	inner = strings.TrimPrefix(inner, "/**")
	inner = strings.TrimPrefix(inner, "/*")
	inner = strings.TrimSuffix(inner, "*/")

	var lines []string
	for _, line := range strings.Split(inner, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
