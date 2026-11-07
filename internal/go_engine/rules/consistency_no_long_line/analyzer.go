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

	"github.com/system-inc/cohere/internal/go_engine/rule"
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

// Limit is the house line length. The fix phase's long-line wrap (internal/line_wrap) wraps to it, so the fix and
// the rule never disagree on what is long.
const Limit = 120

// TabColumns is what a tab counts for. gofmt indents with tabs, and the house reads a tab as four columns wherever it
// sits, rather than as a jump to the next tab stop.
const TabColumns = 4

// closingPunctuation is what may follow a string literal alone on its line: what a formatter prints after a last
// argument or element, with nothing in it that could move to the next line.
const closingPunctuation = ",)}]; \t"

// fieldClosingPunctuation is what may follow a field alone on its line: the comma after it, and the parenthesis or
// brace that closes its list.
const fieldClosingPunctuation = ",)} \t"

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
	for _, long := range source.reportedLines() {
		pass.Report(analysis.Diagnostic{
			Pos:      source.tokenFile.Pos(long.overflow),
			End:      source.tokenFile.Pos(long.end),
			Category: longLineMessage.Id,
			Message: longLineMessage.Render(map[string]string{
				"length": strconv.Itoa(long.length),
				"limit":  strconv.Itoa(Limit),
			}),
		})
	}
}

// ReportedLines are the 1-based lines of a file the rule reports, in order, from content parsed with its comments
// into file and tokenFile. The fix phase's long-line wrap acts on these lines alone, so it never rewrites a line the
// rule leaves alone.
func ReportedLines(file *ast.File, tokenFile *token.File, content []byte) []int {
	reported := newSourceFile(file, tokenFile, content).reportedLines()
	lines := make([]int, 0, len(reported))
	for _, long := range reported {
		lines = append(lines, long.line)
	}
	return lines
}

// HasLongLine is whether any line of content is wider than Limit, the check the fix phase makes before it parses a
// file to wrap.
func HasLongLine(content []byte) bool {
	for line := range bytes.Lines(content) {
		if columnsOf(bytes.TrimRight(line, "\r\n")) > Limit {
			return true
		}
	}
	return false
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
		if columns > Limit {
			return offset
		}
		offset += size
	}
	return len(text)
}

func runeColumns(character rune) int {
	if character == '\t' {
		return TabColumns
	}
	return 1
}
