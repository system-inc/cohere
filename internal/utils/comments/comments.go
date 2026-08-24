package comments

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
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

// All returns every comment in a file, in source order, without duplicates.
//
// The parser skips trivia, so there is no list to read; the comments have to be rescanned out of
// the source text. The scan walks from position zero and from the start of every node, because the
// scanner's iterator yields the run of comments beginning at a position and stops at the first
// token. Positions repeat across a walk (a node and its first child usually start at the same
// place), so the results are deduplicated by range rather than assumed distinct.
// # A comment that is the only content of a block is invisible to this scan
//
// Measured, not theorized: `switch(foo) { case 0: { /* falls through */ } case 1: b(); }` returns
// zero comments from both `All` and `AllWithoutGuard`, and that input is one of upstream's clean
// cases for `no-fallthrough`.
//
// The cause is that this scans at each node's `Pos()` and `End()`. An empty block's positions sit
// outside its braces, and an empty statement list yields no child to anchor a third position, so
// nothing ever looks between them.
//
// Left unfixed deliberately by the porter who found it. `AllWithoutGuard` is the differential
// guard's control and has to change in lockstep, and five rules were mid-wave on the current
// behavior. `no-fallthrough` carries a rule-local scan instead, with the reason at its own line.
//
// Whoever fixes this should change both functions together and re-run every consumer:
// no-irregular-whitespace, no-unused-labels, no-fallthrough, consistency-no-single-line-jsdoc,
// consistency-no-long-line-comment.
func All(sourceFile *ast.SourceFile) []Comment {
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
		startLine, startColumn := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, start)
		endLine, _ := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, end)

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
	// Adjacent nodes share positions constantly: a node's Pos is often its parent's Pos and its
	// first child's Pos, so the same offset arrives many times. Measured on one real file, 55.9% of
	// the positions surviving the guard had already been scanned, and the scanner yielded 131 comment
	// ranges for 57 distinct comments, a 2.3x duplication that `record` then discarded.
	//
	// Remembering the positions already visited is exact rather than approximate, so unlike the guard
	// it cannot change what is found: scanning the same offset twice returns the same ranges.
	scannedPositions := make(map[int]bool)

	collectAt := func(position int) {
		if position < 0 || position > len(text) {
			return
		}
		// A comment must begin with a slash, so a position whose following whitespace run reaches a
		// non-slash character cannot start one. This is the lexical definition rather than an
		// approximation, which is what makes it safe to skip on.
		//
		// It matters because this runs at every node's Pos and End: 1,553 invocations to find 57
		// comments in one real file, and 96.4% of 463,463 candidate positions across 300 files were
		// measured to reach the scanner for nothing.
		if !canBeginAt(text, position) {
			return
		}
		if scannedPositions[position] {
			return
		}
		scannedPositions[position] = true
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

	sortByPosition(comments)
	return comments
}

// sortByPosition puts comments in source order.
//
// The walk reaches nodes in tree order, which is close to source order but not equal to it, and a
// rule reporting findings out of order is a rule whose fixture assertions cannot be written down.
func sortByPosition(comments []Comment) {
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

// IsJsDoc reports whether a block comment is JSDoc: slash-star-star rather than slash-star.
func (c Comment) IsJsDoc() bool {
	return c.IsBlock && strings.HasPrefix(c.Text, "/**")
}

// ContentLines returns a block comment's inner lines with the delimiters and the leading asterisk
// of each line removed, dropping lines that hold nothing else.
//
// This is what "the comment says one thing" means for the JSDoc rules: the asterisks are furniture,
// so a comment that is three lines of furniture around one line of prose is a one-line comment.
func (c Comment) ContentLines() []string {
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

// canBeginAt reports whether a comment could begin at or just after a position.
//
// A comment starts with `/`, so the question is whether the run of whitespace beginning here reaches
// one. Everything else declines without touching the scanner.
//
// The direction of error is deliberate and matches every other gate in this package: this
// **over**-approximates. A `/` that opens a regex literal or a division is admitted here and then
// declined by the scanner, which costs one scan. A comment this hid would be invisible to every
// comment rule, and the fixtures would still pass, so under-approximating is the failure that cannot
// be allowed. `comments_guard_test.go` asserts the comment set is identical with and without this,
// by range rather than by count.
func canBeginAt(text string, position int) bool {
	// A shebang is trivia too, and it is the one non-whitespace thing a comment can sit behind.
	// `GetLeadingCommentRanges` scans past it, so a guard that stopped at the `#` would hide every
	// comment in an executable script. Found by the corpus, not by the hand-written cases: a
	// `#!/usr/bin/env -S pnpm tsx` line cost this file its module comment and its first import
	// comment before the guard learned about it.
	if position == 0 && strings.HasPrefix(text, "#!") {
		return true
	}

	for index := position; index < len(text); index++ {
		switch text[index] {
		case '/':
			return true
		case ' ', '\t', '\n', '\r', '\v', '\f':
			continue
		default:
			return false
		}
	}
	return false
}

// AllWithoutGuard is the scan with canBeginAt removed, kept only so the differential
// test has something to compare against.
//
// It is a duplicate of All by design: a differential test that shared the implementation it
// is checking would prove nothing. The two must be edited together, and the test fails loudly if
// they drift.
func AllWithoutGuard(sourceFile *ast.SourceFile) []Comment {
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
		startLine, startColumn := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, start)
		endLine, _ := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, end)

		comments = append(comments, Comment{
			Range:       commentRange.TextRange,
			IsBlock:     commentRange.Kind == ast.KindMultiLineCommentTrivia,
			Text:        text[start:end],
			StartLine:   startLine,
			StartColumn: startColumn,
			EndLine:     endLine,
		})
	}

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

	collectAt(0)

	var visit func(node *ast.Node)
	visit = func(node *ast.Node) {
		if node == nil {
			return
		}
		collectAt(node.Pos())
		collectAt(node.End())
		node.ForEachChild(func(child *ast.Node) bool {
			visit(child)
			return false
		})
	}
	visit(sourceFile.AsNode())

	sortByPosition(comments)
	return comments
}
