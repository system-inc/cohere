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
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-dash-punctuation"

// listener reads each file's comments as the syntax dispatcher's walk enters the file, and hears every literal in it.
var listener = rule.Listener{
	Requires:  []*analysis.Analyzer{positions.Analyzer},
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.BasicLit)(nil)},
	Node:      judgeLiteral,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_dash_punctuation", "Flags each em-dash or en-dash "+
	"written as itself in a comment or a string literal, because readers clock them as generated text and tune out. "+
	"A dash spelled with an escape is data and is left alone, so a dash in an interpreted string comes with a fix "+
	"that rewrites it as its escape, keeping the value the same. A dash in a raw string, which cannot hold an "+
	"escape, or in a comment is reported with no fix.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer, Listener: &listener}

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

// fileState is what the rule holds of the file the walk is in, in its context's state.
type fileState struct {
	// table is the package's line table, which reads a comment's or a literal's line without the FileSet's lock.
	table *positions.Table
	// tokenFile is the file being walked. Each comment and literal starts a line in it, so its line table is looked up
	// once for all of them.
	tokenFile *token.File
}

// enterFile reads the line table the first time, looks up the file's own line table, and reports the dashes in
// file's comments.
func enterFile(context *rule.Context, file *ast.File) {
	state := rule.State[fileState](context)
	if state.table == nil {
		table, isTable := context.ResultOf(positions.Analyzer).(*positions.Table)
		if !isTable {
			// Invariant: the listener requires positions, whose result is always a table, so anything else is a
			// mistake in building the rule, which the dispatcher's recover makes this rule's crash on the package.
			panic(fmt.Sprintf("reading the line table: positions gave %T", context.ResultOf(positions.Analyzer)))
		}
		state.table = table
	}
	state.tokenFile = context.Fset.File(file.FileStart)
	for _, group := range file.Comments {
		for _, comment := range group.List {
			// comment.Text is the comment's source from its first slash, so it starts at comment.Pos().
			reportDashes(context, state.table, state.tokenFile, comment.Pos(), comment.Text, commentText)
		}
	}
}

// judgeLiteral reports the dashes in a string literal's source.
func judgeLiteral(context *rule.Context, node ast.Node, _ []ast.Node) {
	literal, isLiteral := node.(*ast.BasicLit)
	// A rune literal names a character, as this rule's own constants do, so only strings are read.
	if !isLiteral || literal.Kind != token.STRING {
		return
	}
	state := rule.State[fileState](context)
	// literal.Value is the literal's source, quotes included, so it starts at literal.Pos(). Only a dash written as
	// itself is in the source as a dash, so an escaped one is never seen here.
	if strings.HasPrefix(literal.Value, "`") {
		reportDashes(context, state.table, state.tokenFile, literal.Pos(), literal.Value, rawStringText)
	} else {
		reportDashes(context, state.table, state.tokenFile, literal.Pos(), literal.Value, interpretedStringText)
	}
}

// reportDashes reports each dash written as itself in source, the source text starting at start in tokenFile, as kind
// decides. start's line is read from table, without the FileSet's lock, which every comment and literal would take.
func reportDashes(
	context *rule.Context,
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
		context.Report(diagnostic)
	}
}
