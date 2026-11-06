package consistency_no_long_line

import (
	"bytes"
	"cmp"
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// sourceFile is one file as the rule reads it: its text, its line table, and what of its syntax tree the exemptions
// need, each gathered once.
type sourceFile struct {
	tokenFile *token.File
	content   []byte
	// exemptLines are the lines no length is blamed on: an import spec's, and a //go: directive's.
	exemptLines map[int]bool
	// stringLiterals and comments are in source order, so the one holding an offset is a binary search away.
	stringLiterals []*ast.BasicLit
	comments       []*ast.Comment
	// unwrappableOpenings maps the offset a line's code opens at to the string literal that line may overrun on,
	// when nothing from that offset to the literal could move to a line of its own: the literal itself, a struct
	// field's names and type before its tag, the return keyword before a lone returned string, since a newline
	// after return makes it a bare return, or a key or a name before its operator and a lone string.
	unwrappableOpenings map[int]*ast.BasicLit
	// unwrappableFields maps the offset a parameter, a result or a struct field opens at to that field, when it has at
	// most one name, no tag, and a type gofmt rejoins however it is broken.
	unwrappableFields map[int]*ast.Field
}

// isRawStringBody is whether the overflow falls in a raw string's body on a line after the one the string opens on:
// data with its own line structure, which a break would change. On the line the string closes on, code after it is
// code like any other and could wrap, so only closing punctuation may follow.
func (source *sourceFile) isRawStringBody(line int, overflow int) bool {
	literal, found := nodeHolding(source, source.stringLiterals, overflow)
	if !found || !strings.HasPrefix(literal.Value, "`") || source.lineOf(literal.Pos()) >= line {
		return false
	}
	return source.closesTheLine(literal, line)
}

// isLoneStringLiteral is whether the line's code opens where nothing before a string literal could wrap, with only
// closing punctuation after the literal and the overflow in the literal or that punctuation: the shape a formatter
// prints for a long string, a long tag, a long returned string or a long string given to a key or a name, which leaves
// nothing to break but the string. Code before that, a comment ahead of the literal, overflowing ahead of the literal,
// or more code after it, could move or be shortened, so is a finding.
func (source *sourceFile) isLoneStringLiteral(line int, overflow int) bool {
	literal, found := source.unwrappableOpenings[source.codeStart(line)]
	if !found || source.lineOf(literal.Pos()) != line || overflow < source.offsetOf(literal.Pos()) {
		return false
	}
	// A comment ahead of the literal could sit on the line above instead.
	if source.opensComment(source.codeStart(line), source.offsetOf(literal.Pos())) {
		return false
	}
	return source.closesTheLine(literal, line)
}

// isLoneField is whether line holds, past its indentation, one field from unwrappableFields that opens and ends on it,
// with no comment inside it and only closing punctuation after it. A comment, more code or the opening of a body after
// the field could move to a line of its own, so is a finding.
func (source *sourceFile) isLoneField(line int) bool {
	field, found := source.unwrappableFields[source.codeStart(line)]
	if !found || source.lineOf(field.End()) != line {
		return false
	}
	fieldEnd := source.offsetOf(field.End())
	if source.opensComment(source.codeStart(line), fieldEnd) {
		return false
	}
	return len(bytes.Trim(source.content[fieldEnd:source.lineEnd(line)], fieldClosingPunctuation)) == 0
}

// gatherUnwrappableFields records each field of fields, a parameter, result or struct field list that may be absent,
// that has at most one name, no tag, and a type gofmt cannot break. Two names could wrap after their comma. A tag is
// the tag exemption's to judge, which leaves alone only an overflow inside the tag, so a field that overruns before
// its tag opens is still a finding.
func (source *sourceFile) gatherUnwrappableFields(fields *ast.FieldList) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		if len(field.Names) <= 1 && field.Tag == nil && isUnbreakableType(field.Type) {
			source.unwrappableFields[source.offsetOf(field.Pos())] = field
		}
	}
}

// opensComment is whether a comment opens at an offset from start up to, not including, end.
func (source *sourceFile) opensComment(start int, end int) bool {
	index, _ := slices.BinarySearchFunc(source.comments, start, func(comment *ast.Comment, target int) int {
		return cmp.Compare(source.offsetOf(comment.Pos()), target)
	})
	return index < len(source.comments) && source.offsetOf(source.comments[index].Pos()) < end
}

// closesTheLine is whether nothing but closing punctuation follows literal on line, which holds as well for a raw
// string that runs on past it.
func (source *sourceFile) closesTheLine(literal *ast.BasicLit, line int) bool {
	lineEnd := source.lineEnd(line)
	literalEnd := source.offsetOf(literal.End())
	if literalEnd >= lineEnd {
		return true
	}
	return len(bytes.Trim(source.content[literalEnd:lineEnd], closingPunctuation)) == 0
}

// isUnbreakableCommentToken is whether the overflow falls in a comment on one token with no space in it, such as a
// URL, that would not fit even on a comment line of its own, so wrapping the comment cannot bring the line in. A word
// that would fit there is the comment's to wrap, as is any space past the limit.
func (source *sourceFile) isUnbreakableCommentToken(line int, overflow int) bool {
	comment, found := nodeHolding(source, source.comments, overflow)
	if !found {
		return false
	}
	lineEnd := source.lineEnd(line)
	commentEnd := min(source.offsetOf(comment.End()), lineEnd)
	// Code after a block comment that closes on this line is more past the limit, and code wraps.
	if len(bytes.TrimSpace(source.content[commentEnd:lineEnd])) > 0 {
		return false
	}
	tail := source.content[overflow:commentEnd]
	if commentEnd == source.offsetOf(comment.End()) && strings.HasPrefix(comment.Text, "/*") {
		tail = bytes.TrimSuffix(tail, []byte("*/"))
	}
	tail = bytes.TrimRight(tail, " \t")
	if len(tail) == 0 || bytes.ContainsAny(tail, " \t") {
		return false
	}
	// The token opens after the last space before the overflow, and no earlier than the comment's text on this line:
	// past its slashes when the comment opens here, or the line's start when a block comment began above.
	textStart := max(source.offsetOf(comment.Pos())+len("//"), source.lineStart(line))
	// An overflow on the comment's own slashes is a comment that opens past the limit, which can move whole.
	if overflow < textStart {
		return false
	}
	tokenStart := textStart + bytes.LastIndexAny(source.content[textStart:overflow], " \t") + 1
	indentation := source.content[source.lineStart(line):source.codeStart(line)]
	ownLine := columnsOf(indentation) + len("// ") + columnsOf(source.content[tokenStart:overflow+len(tail)])
	return ownLine > limit
}

func (source *sourceFile) offsetOf(position token.Pos) int {
	return source.tokenFile.Offset(position)
}

// lineOf is the line position sits on in the file as written, not one a //line directive gives it, since the rule
// measures the text on disk.
func (source *sourceFile) lineOf(position token.Pos) int {
	return source.tokenFile.PositionFor(position, false).Line
}

func (source *sourceFile) lineStart(line int) int {
	return source.offsetOf(source.tokenFile.LineStart(line))
}

// lineEnd is the offset just past line's last character, before its newline and a carriage return ahead of it.
func (source *sourceFile) lineEnd(line int) int {
	end := len(source.content)
	if line < source.tokenFile.LineCount() {
		end = source.lineStart(line + 1)
	}
	text := bytes.TrimSuffix(bytes.TrimSuffix(source.content[source.lineStart(line):end], []byte("\n")), []byte("\r"))
	return source.lineStart(line) + len(text)
}

// codeStart is the offset of line's first character past its indentation.
func (source *sourceFile) codeStart(line int) int {
	offset := source.lineStart(line)
	for offset < len(source.content) && (source.content[offset] == ' ' || source.content[offset] == '\t') {
		offset++
	}
	return offset
}
