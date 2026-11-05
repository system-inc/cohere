// Package consistency_no_long_line is the Go engine's rule that a line fits in 120 columns.
//
// A line past the limit runs off the edge of a review pane or a split editor, and its end goes unread. gofmt never
// wraps a line, so the limit is the writer's to keep. What cannot wrap is left alone: an import path, a //go:
// directive, a raw string's body, a string literal alone on its line or after only a struct field's
// names and type or a return, and a comment that overruns only on one token too long for a comment line of its own,
// such as a URL.
package consistency_no_long_line

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-long-line"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_long_line",
	Doc: "Flags a line longer than 120 columns, a tab counted as 4 and every other rune as 1, at its 121st column. " +
		"An import line, a //go: directive, a raw string's body, a string literal alone on its " +
		"line or after only a struct field's names and type or a return, and a comment that overruns only on one " +
		"token too long to wrap, such as a URL, are left alone.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads the syntax tree and the source, never the types.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var longLineMessage = policy.MessageOf(Name, "longLine")

// limit is the house line length.
const limit = 120

// tabColumns is what a tab counts for. gofmt indents with tabs, and the house reads a tab as four columns wherever it
// sits, rather than as a jump to the next tab stop.
const tabColumns = 4

// closingPunctuation is what may follow a string literal alone on its line: what a formatter prints after a last
// argument or element, with nothing in it that could move to the next line.
const closingPunctuation = ",)}]; \t"

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
	// field's names and type before its tag, or the return keyword before a lone returned string, since a newline
	// after return makes it a bare return.
	unwrappableOpenings map[int]*ast.BasicLit
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.FileStart)
		content, readError := pass.ReadFile(tokenFile.Name())
		if readError != nil {
			return nil, fmt.Errorf("reading %s: %w", tokenFile.Name(), readError)
		}
		// Every offset the syntax tree holds is into the text it was parsed from, so a file that changed on disk
		// since then cannot be measured against them.
		if len(content) != tokenFile.Size() {
			return nil, fmt.Errorf("reading %s: it holds %d bytes and was parsed from %d", tokenFile.Name(),
				len(content), tokenFile.Size())
		}
		judgeFile(pass, newSourceFile(file, tokenFile, content))
	}
	return nil, nil
}

func newSourceFile(file *ast.File, tokenFile *token.File, content []byte) *sourceFile {
	source := &sourceFile{
		tokenFile:           tokenFile,
		content:             content,
		exemptLines:         map[int]bool{},
		unwrappableOpenings: map[int]*ast.BasicLit{},
	}
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.IMPORT {
			continue
		}
		// An import path is not ours to wrap, and Go has no way to split one. The block's own comment lines are
		// prose like any other, so only the specs' lines are exempt.
		for _, spec := range general.Specs {
			for line := source.lineOf(spec.Pos()); line <= source.lineOf(spec.End()); line++ {
				source.exemptLines[line] = true
			}
		}
	}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			source.comments = append(source.comments, comment)
			// The toolchain reads a //go: directive as one line, so it cannot wrap. It counts only where it opens its
			// line, since one after code is no directive.
			if strings.HasPrefix(comment.Text, "//go:") && source.codeStart(source.lineOf(comment.Pos())) ==
				source.offsetOf(comment.Pos()) {
				source.exemptLines[source.lineOf(comment.Pos())] = true
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				source.stringLiterals = append(source.stringLiterals, node)
				source.unwrappableOpenings[source.offsetOf(node.Pos())] = node
			}
		case *ast.Field:
			// A field's names, type and tag are one declaration Go reads to the end of its line, and the tag is
			// one literal, so a line holding only them has nothing that could wrap.
			if node.Tag != nil {
				source.unwrappableOpenings[source.offsetOf(node.Pos())] = node.Tag
			}
		case *ast.ReturnStmt:
			if len(node.Results) == 1 {
				if literal, isLiteral := node.Results[0].(*ast.BasicLit); isLiteral && literal.Kind == token.STRING {
					source.unwrappableOpenings[source.offsetOf(node.Pos())] = literal
				}
			}
		}
		return true
	})
	// Inspect visits in source order already; the sort keeps the binary search honest without leaning on that.
	slices.SortFunc(source.stringLiterals, func(left, right *ast.BasicLit) int {
		return cmp.Compare(left.Pos(), right.Pos())
	})
	return source
}

// judgeFile reports every line of source past the limit that no exemption covers, at its first column past it.
func judgeFile(pass *analysis.Pass, source *sourceFile) {
	for line := 1; line <= source.tokenFile.LineCount(); line++ {
		if source.exemptLines[line] {
			continue
		}
		start, end := source.lineStart(line), source.lineEnd(line)
		length := columnsOf(source.content[start:end])
		if length <= limit {
			continue
		}
		overflow := start + offsetPastLimit(source.content[start:end])
		if source.isRawStringBody(line, overflow) || source.isLoneStringLiteral(line, overflow) ||
			source.isUnbreakableCommentToken(line, overflow) {
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:      source.tokenFile.Pos(overflow),
			End:      source.tokenFile.Pos(end),
			Category: longLineMessage.Id,
			Message: longLineMessage.Render(map[string]string{
				"length": strconv.Itoa(length),
				"limit":  strconv.Itoa(limit),
			}),
		})
	}
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
// prints for a long string, a long tag or a long returned string, which leaves nothing to break but the string. Code
// before that, overflowing ahead of the literal, or more code after it, could move or be shortened, so is a finding.
func (source *sourceFile) isLoneStringLiteral(line int, overflow int) bool {
	literal, found := source.unwrappableOpenings[source.codeStart(line)]
	if !found || source.lineOf(literal.Pos()) != line || overflow < source.offsetOf(literal.Pos()) {
		return false
	}
	return source.closesTheLine(literal, line)
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

// nodeHolding is the node of nodes, which are in source order and do not overlap, whose text holds offset.
func nodeHolding[Node ast.Node](source *sourceFile, nodes []Node, offset int) (Node, bool) {
	index, found := slices.BinarySearchFunc(nodes, offset, func(node Node, target int) int {
		return cmp.Compare(source.offsetOf(node.Pos()), target)
	})
	// Without an exact match, index is the first node starting after offset, so the one before it is the candidate.
	if !found {
		index--
	}
	if index < 0 || offset >= source.offsetOf(nodes[index].End()) {
		var none Node
		return none, false
	}
	return nodes[index], true
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

// columnsOf is how wide text reads: a tab as tabColumns and every other rune, not byte, as one, so a line of
// multi-byte letters is measured by what a reader sees.
func columnsOf(text []byte) int {
	columns := 0
	for offset := 0; offset < len(text); {
		character, size := utf8.DecodeRune(text[offset:])
		columns += runeColumns(character)
		offset += size
	}
	return columns
}

// offsetPastLimit is the byte offset in text of the rune that first reaches past the limit, a tab that straddles it
// included, or len(text) when none does.
func offsetPastLimit(text []byte) int {
	columns := 0
	for offset := 0; offset < len(text); {
		character, size := utf8.DecodeRune(text[offset:])
		columns += runeColumns(character)
		if columns > limit {
			return offset
		}
		offset += size
	}
	return len(text)
}

func runeColumns(character rune) int {
	if character == '\t' {
		return tabColumns
	}
	return 1
}
