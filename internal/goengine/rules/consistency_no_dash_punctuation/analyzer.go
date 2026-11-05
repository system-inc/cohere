// Package consistency_no_dash_punctuation is the Go engine's rule that the house writes no em-dash or en-dash.
//
// Readers clock the em-dash (U+2014) and the en-dash (U+2013) as generated text and tune out of the words around
// them, so a comment or a string a person will read uses a comma, a period, or a restructured sentence instead. The
// rule reads every comment and every string literal's value, raw or interpreted, and reports each dash at the rune
// itself. A dash an interpreted string spells with an escape is in the value all the same, so it is reported too, at
// the literal, since the escape is where the source holds it. A hyphen-minus and the minus sign (U+2212) are other
// characters and are never read as dashes.
package consistency_no_dash_punctuation

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-dash-punctuation"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_dash_punctuation",
	Doc: "Flags each em-dash or en-dash in a comment or a string literal's value, because readers clock them as " +
		"generated text and tune out.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var (
	dashPunctuationMessage        = policy.MessageOf(Name, "dashPunctuation")
	escapedDashPunctuationMessage = policy.MessageOf(Name, "escapedDashPunctuation")
)

// The two runes the rule reports. They are spelled as escapes in rune literals, which the rule does not read, so
// the rule's own source holds no dash.
const (
	emDash = '\u2014'
	enDash = '\u2013'
)

// dashNames is how a message names each dash, in the order the rule reports escaped ones.
var dashNames = []struct {
	dash rune
	name string
}{
	{dash: emDash, name: "an em-dash"},
	{dash: enDash, name: "an en-dash"},
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				// comment.Text is the comment's source from its first slash, so it starts at comment.Pos().
				reportRawDashes(pass, comment.Pos(), comment.Text)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, isLiteral := node.(*ast.BasicLit)
			// A rune literal names a character, as this rule's own constants do, so only strings are read.
			if isLiteral && literal.Kind == token.STRING {
				judgeString(pass, literal)
			}
			return true
		})
	}
	return nil, nil
}

// judgeString reports the dashes in a string literal's value: each one written as itself at its own rune, and each
// one an escape spells at the literal.
func judgeString(pass *analysis.Pass, literal *ast.BasicLit) {
	// literal.Value is the literal's source, quotes included, so it starts at literal.Pos(). A raw string has no
	// escapes, so every dash in its value is one of these.
	rawCounts := reportRawDashes(pass, literal.Pos(), literal.Value)
	if strings.HasPrefix(literal.Value, "`") {
		return
	}
	value, unquoteError := strconv.Unquote(literal.Value)
	// A literal that does not unquote did not parse either, and the type checker reports it; there is no value
	// to read.
	if unquoteError != nil {
		return
	}
	// A dash written as itself in an interpreted string is the same dash in its value, so whatever the value
	// holds beyond those came from an escape: \u2014, \U00002014, or its three bytes as \x or octal escapes.
	for _, dashName := range dashNames {
		escaped := strings.Count(value, string(dashName.dash)) - rawCounts[dashName.dash]
		for range escaped {
			pass.Report(analysis.Diagnostic{
				Pos:      literal.Pos(),
				End:      literal.End(),
				Category: escapedDashPunctuationMessage.Id,
				Message:  escapedDashPunctuationMessage.Render(map[string]string{"dash": dashName.name}),
			})
		}
	}
}

// reportRawDashes reports each dash written as itself in source, the source text starting at start, and returns
// how many of each it found.
func reportRawDashes(pass *analysis.Pass, start token.Pos, source string) map[rune]int {
	counts := map[rune]int{}
	// The scanner drops every carriage return from a block comment's and a raw string's text, so in a file with CRLF
	// line endings an offset into source falls one byte short of the file for each line it crosses. Each line is
	// placed at the file's own start of that line instead, from the line table the scanner built from the real
	// bytes. The line is the file's own, not one a //line directive gives, since LineStart counts the file's lines.
	tokenFile := pass.Fset.File(start)
	line := tokenFile.PositionFor(start, false).Line
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
		counts[character]++
		position := lineStart + token.Pos(offset-lineOffset)
		pass.Report(analysis.Diagnostic{
			Pos:      position,
			End:      position + token.Pos(utf8.RuneLen(character)),
			Category: dashPunctuationMessage.Id,
			Message:  dashPunctuationMessage.Render(map[string]string{"dash": name}),
		})
	}
	return counts
}
