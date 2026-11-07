// Package consistency_no_dash_punctuation is the Go engine's rule that the house writes no em-dash or en-dash.
//
// Readers clock the em-dash (U+2014) and the en-dash (U+2013) as generated text and tune out of the words around
// them, so a comment or a string a person will read uses a comma, a period, or a restructured sentence instead. The
// rule reads every comment and every string literal's source, and reports each dash written as itself, at the dash.
//
// A dash a string spells with an escape is data, not prose: a test of how em-dashes are handled needs one, and the
// escape is how its writer says the dash is meant. So an escaped dash is never reported, and a dash written as itself
// in an interpreted string comes with a fix that rewrites it as its escape. The value holds the same bytes either way,
// so the fix changes nothing the program does, only what a reader can see. A raw string cannot hold an escape, so its
// dash is reported with no fix, and neither is a comment's. A hyphen-minus and the minus sign (U+2212) are other
// characters and are never read as dashes.
package consistency_no_dash_punctuation

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-dash-punctuation"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_dash_punctuation",
	Doc: "Flags each em-dash or en-dash written as itself in a comment or a string literal, because readers clock " +
		"them as generated text and tune out. A dash spelled with an escape is data and is left alone, so a dash " +
		"in an interpreted string comes with a fix that rewrites it as its escape, keeping the value the same. A " +
		"dash in a raw string, which cannot hold an escape, or in a comment is reported with no fix.",
	Requires: []*analysis.Analyzer{positions.Analyzer},
	Run:      run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var dashPunctuationMessage = policy.MessageOf(Name, "dashPunctuation")

var dashPunctuationInStringMessage = policy.MessageOf(Name, "dashPunctuationInString")

var dashPunctuationInRawStringMessage = policy.MessageOf(Name, "dashPunctuationInRawString")

// The two runes the rule reports. They are spelled as escapes in rune literals, which the rule does not read, so
// the rule's own source holds no dash.
const (
	emDash = '\u2014'
	enDash = '\u2013'
)

// dashNames is how a message names each dash.
var dashNames = []struct {
	dash rune
	name string
}{
	{dash: emDash, name: "an em-dash"},
	{dash: enDash, name: "an en-dash"},
}

// textKind is where a dash was found, which decides what its finding says and whether it offers a fix.
type textKind int

const (
	commentText textKind = iota
	interpretedStringText
	rawStringText
)

func run(pass *analysis.Pass) (any, error) {
	table, isTable := pass.ResultOf[positions.Analyzer].(*positions.Table)
	if !isTable {
		return nil, fmt.Errorf("reading the line table: positions gave %T", pass.ResultOf[positions.Analyzer])
	}
	for _, file := range pass.Files {
		// Each comment and literal starts a line in this file, so its line table is looked up once for all of them.
		tokenFile := pass.Fset.File(file.FileStart)
		for _, group := range file.Comments {
			for _, comment := range group.List {
				// comment.Text is the comment's source from its first slash, so it starts at comment.Pos().
				reportDashes(pass, table, tokenFile, comment.Pos(), comment.Text, commentText)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, isLiteral := node.(*ast.BasicLit)
			// A rune literal names a character, as this rule's own constants do, so only strings are read.
			if !isLiteral || literal.Kind != token.STRING {
				return true
			}
			// literal.Value is the literal's source, quotes included, so it starts at literal.Pos(). Only a dash
			// written as itself is in the source as a dash, so an escaped one is never seen here.
			if strings.HasPrefix(literal.Value, "`") {
				reportDashes(pass, table, tokenFile, literal.Pos(), literal.Value, rawStringText)
			} else {
				reportDashes(pass, table, tokenFile, literal.Pos(), literal.Value, interpretedStringText)
			}
			return true
		})
	}
	return nil, nil
}

// reportDashes reports each dash written as itself in source, the source text starting at start in tokenFile, as kind
// decides. start's line is read from table, without the FileSet's lock, which every comment and literal would take.
func reportDashes(
	pass *analysis.Pass,
	table *positions.Table,
	tokenFile *token.File,
	start token.Pos,
	source string,
	kind textKind,
) {
	// The scanner drops every carriage return from a block comment's and a raw string's text, so in a file with CRLF
	// line endings an offset into source falls one byte short of the file for each line it crosses. Each line is
	// placed at the file's own start of that line instead, from the line table the scanner built from the real
	// bytes. The line is the file's own, not one a //line directive gives, since LineStart counts the file's lines.
	line := table.Line(start)
	lineStart := start
	// lineOffset is where the current line begins in source.
	lineOffset := 0
	for offset, character := range source {
		// Every newline in a comment or a literal is one the scanner added to the file's line table, so the next
		// line is always there to start.
		if character == '\n' {
			line++
			lineStart = tokenFile.LineStart(line)
			lineOffset = offset + 1
			continue
		}
		name := ""
		for _, dashName := range dashNames {
			if character == dashName.dash {
				name = dashName.name
			}
		}
		if name == "" {
			continue
		}
		position := lineStart + token.Pos(offset-lineOffset)
		diagnostic := analysis.Diagnostic{
			Pos:      position,
			End:      position + token.Pos(utf8.RuneLen(character)),
			Category: dashPunctuationMessage.Id,
			Message:  dashPunctuationMessage.Render(map[string]string{"dash": name}),
		}
		// The escape strconv writes for the rune, without the quotes around it: the dash's own code point, which
		// unquotes to the same bytes the dash is written with.
		escape := strings.Trim(strconv.QuoteToASCII(string(character)), `"`)
		switch kind {
		case interpretedStringText:
			diagnostic.Category = dashPunctuationInStringMessage.Id
			diagnostic.Message = dashPunctuationInStringMessage.Render(
				map[string]string{"dash": name, "escape": escape},
			)
			diagnostic.SuggestedFixes = []analysis.SuggestedFix{{
				Message: "Write " + name + " as " + escape,
				TextEdits: []analysis.TextEdit{{
					Pos:     diagnostic.Pos,
					End:     diagnostic.End,
					NewText: []byte(escape),
				}},
			}}
		case rawStringText:
			diagnostic.Category = dashPunctuationInRawStringMessage.Id
			diagnostic.Message = dashPunctuationInRawStringMessage.Render(
				map[string]string{"dash": name, "escape": escape},
			)
		case commentText:
			// A comment is prose with no value to keep, so the dash is rewritten by hand, as the message says.
		}
		pass.Report(diagnostic)
	}
}
