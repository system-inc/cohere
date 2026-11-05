// Package consistency_no_long_line is the Go engine's rule that a line fits in 120 columns.
//
// A line past the limit runs off the edge of a review pane or a split editor, and its end goes unread. gofmt never
// wraps a line, so the limit is the writer's to keep. What cannot wrap is left alone: an import path, a //go:
// directive, a raw string's body, a string literal alone on its line or after only a struct field's names and type, a
// return, or a key or a name and its operator, and a comment that overruns only on one token too long for a comment
// line of its own, such as a URL.
//
// A parameter, a result or a struct field alone on its line is left alone too, when it has at most one name, no tag,
// and a type gofmt cannot break: names, selectors, pointers, slices, arrays, maps, channels, parentheses and an index
// with one type argument. gofmt rejoins such a type however it is broken, save after a selector's dot, which the house
// already counts as no cure for a key, so flagging the line buys no readability, and a new name or alias added only for
// width is no cure either. A func type, a struct or interface literal, two type arguments or two names can all wrap,
// so they are still judged, and a field with a tag is left to the tag's own exemption above.
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
		"An import line, a //go: directive, a raw string's body, a string literal alone on its line or after only a " +
		"struct field's names and type, a return, or a key or a name and its operator, a parameter, result or struct " +
		"field alone on its line with at most one name, no tag and a type gofmt cannot break, and a comment that " +
		"overruns only on one token too long to wrap, such as a URL, are left alone.",
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

// fieldClosingPunctuation is what may follow a field alone on its line: the comma after it, and the parenthesis or
// brace that closes its list.
const fieldClosingPunctuation = ",)} \t"

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
		unwrappableFields:   map[int]*ast.Field{},
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
				// Inspect visits a parent first, so a string key already maps to the value it opens a line for, which
				// reaches further than the key alone and must not be lost to it.
				if _, mapped := source.unwrappableOpenings[source.offsetOf(node.Pos())]; !mapped {
					source.unwrappableOpenings[source.offsetOf(node.Pos())] = node
				}
			}
		case *ast.Field:
			// A field's names, type and tag are one declaration Go reads to the end of its line, and the tag is
			// one literal, so a line holding only them has nothing that could wrap.
			if node.Tag != nil {
				source.unwrappableOpenings[source.offsetOf(node.Pos())] = node.Tag
			}
		case *ast.FuncType:
			// A type parameter list is left out: it is not a parameter or a result, and the ruling is for those.
			source.gatherUnwrappableFields(node.Params)
			source.gatherUnwrappableFields(node.Results)
		case *ast.StructType:
			source.gatherUnwrappableFields(node.Fields)
		case *ast.ReturnStmt:
			if len(node.Results) == 1 {
				if literal, isLiteral := stringLiteralOf(node.Results[0]); isLiteral {
					source.unwrappableOpenings[source.offsetOf(node.Pos())] = literal
				}
			}
		case *ast.KeyValueExpr:
			// gofmt prints a composite literal's key and its value on one line however they were written, and
			// Prettier prints a TypeScript key and its string the same way, so a key that is only a name or a
			// literal leaves nothing before its string that could wrap.
			if literal, isLiteral := stringLiteralOf(node.Value); isLiteral && isUnwrappableKey(node.Key) {
				source.unwrappableOpenings[source.offsetOf(node.Key.Pos())] = literal
			}
		case *ast.AssignStmt:
			// One name given one string is the shape Prettier keeps on one line in TypeScript, and the house holds
			// Go to the same parity. Two names, an index or a compound operator such as += are code, and wrap.
			if (node.Tok == token.ASSIGN || node.Tok == token.DEFINE) && len(node.Lhs) == 1 && len(node.Rhs) == 1 &&
				isName(node.Lhs[0]) {
				if literal, isLiteral := stringLiteralOf(node.Rhs[0]); isLiteral {
					source.unwrappableOpenings[source.offsetOf(node.Lhs[0].Pos())] = literal
				}
			}
		case *ast.ValueSpec:
			// A var or const spec in a block opens its line at its name, and gofmt rejoins the name and its value.
			if literal, isLiteral := namedStringOf(node); isLiteral {
				source.unwrappableOpenings[source.offsetOf(node.Pos())] = literal
			}
		case *ast.GenDecl:
			// An ungrouped var or const opens its line at its keyword, which cannot leave its name, as return cannot
			// leave its result.
			if (node.Tok == token.VAR || node.Tok == token.CONST) && !node.Lparen.IsValid() && len(node.Specs) == 1 {
				if spec, isValueSpec := node.Specs[0].(*ast.ValueSpec); isValueSpec {
					if literal, isLiteral := namedStringOf(spec); isLiteral {
						source.unwrappableOpenings[source.offsetOf(node.Pos())] = literal
					}
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
			source.isUnbreakableCommentToken(line, overflow) || source.isLoneField(line) {
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

// isUnbreakableType is whether a type expression has no place gofmt keeps a line break: names, selectors, pointers,
// slices, arrays of a literal length, maps, channels and parentheses over those, and an index with one type argument.
// gofmt rejoins every one of them however it is broken. A func type, a struct or interface literal and an index with
// two or more type arguments can break after a comma or a brace, so they are not here.
func isUnbreakableType(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		return isName(expression)
	case *ast.StarExpr:
		return isUnbreakableType(expression.X)
	case *ast.ArrayType:
		// A slice has no length, and an array's length is unbreakable only as a literal; [...] or a constant
		// expression is code.
		if expression.Len != nil {
			if _, isLiteral := expression.Len.(*ast.BasicLit); !isLiteral {
				return false
			}
		}
		return isUnbreakableType(expression.Elt)
	case *ast.MapType:
		return isUnbreakableType(expression.Key) && isUnbreakableType(expression.Value)
	case *ast.ChanType:
		return isUnbreakableType(expression.Value)
	case *ast.ParenExpr:
		return isUnbreakableType(expression.X)
	case *ast.IndexExpr:
		return isUnbreakableType(expression.X) && isUnbreakableType(expression.Index)
	}
	return false
}

// opensComment is whether a comment opens at an offset from start up to, not including, end.
func (source *sourceFile) opensComment(start int, end int) bool {
	index, _ := slices.BinarySearchFunc(source.comments, start, func(comment *ast.Comment, target int) int {
		return cmp.Compare(source.offsetOf(comment.Pos()), target)
	})
	return index < len(source.comments) && source.offsetOf(source.comments[index].Pos()) < end
}

// stringLiteralOf is expression as a string literal, interpreted or raw, when it is one.
func stringLiteralOf(expression ast.Expr) (*ast.BasicLit, bool) {
	literal, isLiteral := expression.(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return nil, false
	}
	return literal, true
}

// namedStringOf is the string a var or const spec gives its one name, with no type between them, since a type is
// more that could be long.
func namedStringOf(spec *ast.ValueSpec) (*ast.BasicLit, bool) {
	if len(spec.Names) != 1 || spec.Type != nil || len(spec.Values) != 1 {
		return nil, false
	}
	return stringLiteralOf(spec.Values[0])
}

// isName is whether expression is a name or a chain of them, such as record.Name: nothing in it could wrap.
func isName(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return isName(expression.X)
	}
	return false
}

// isUnwrappableKey is whether a composite literal's key is a name or a basic literal, such as a map's string key,
// with nothing in it that could wrap. A key built by a call or an operation is code.
func isUnwrappableKey(key ast.Expr) bool {
	if _, isLiteral := key.(*ast.BasicLit); isLiteral {
		return true
	}
	return isName(key)
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
