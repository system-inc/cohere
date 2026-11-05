// Package consistency_no_shouting is the Go engine's rule that a comment speaks in a normal voice.
//
// All-caps reads as aggression, and it spreads: a reader mirrors the register of what they are answering, so one
// shouted comment ratchets a file's comments into block capitals until emphasis means nothing. The judgment of what
// counts as shouting, the acronym safe list and the masks that hide code, lives in internal/shouting, which the
// TypeScript rule nexus/consistency-no-shouting reads too, so the two engines agree on what an acronym is.
//
// Go writes its doc comments as a run of // lines where TypeScript writes one block comment, so a comment group is
// masked as one text, each // line's slashes and the one space after them set aside the way a block comment's
// gutter is. A fence, an indented code block, or a quote or backtick pair that wraps across a group's lines is then
// code here as it is inside a block comment, and a command line on a // line is masked. Each comment still reports
// only its own tokens, at itself. Go's own notations are not prose and are not read: directives, go/doc notes, an
// example's output, and the marker a generated file opens with.
package consistency_no_shouting

import (
	"go/ast"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/shouting"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-shouting"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_shouting",
	Doc: "Flags a comment holding an all-caps word that is not an acronym on the shared safe list, because shouting " +
		"spreads to the next reader's comments. Code in backticks or quotes, directives, go/doc notes, an " +
		"example's output and a generated file's `// Code generated ... DO NOT EDIT.` marker are not read.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var shoutingInCommentMessage = policy.MessageOf(Name, "shoutingInComment")

var (
	// directive is go/doc's own definition of a directive comment, from go.dev/doc/comment: a line starting
	// //line, //extern or //export and a space, or a tool's //name:directive with no space after the slashes. It
	// is an instruction to a tool, //go:generate or //lint:ignore, and its arguments are the tool's words.
	directive = regexp.MustCompile(`^//(line |extern |export |[a-z0-9]+:[a-z0-9])`)

	// noteMarker is go/doc's note, `MARKER(uid)`, where it opens a comment: two or more capitals naming the kind
	// of note, `BUG(who)` the one go doc lists, and the uid in parentheses. go/doc collects these into their own
	// section, so the marker is notation; the note's body after it is prose and is read.
	noteMarker = regexp.MustCompile(`^/[/*][ \t]*([A-Z][A-Z]+\([^)]+\))`)

	// exampleOutput is go/doc's test for the comment that holds an example's expected output, which is whatever
	// the example prints, capitals and all.
	exampleOutput = regexp.MustCompile(`(?i)^[[:space:]]*(unordered )?output:`)

	// generatedMarker is the line go.dev/s/generatedcode says marks a file as generated, spelled exactly so tools
	// can find it. Its capitals are the convention's, which no writer can lower, so it is notation.
	generatedMarker = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)
)

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		outputs := exampleOutputGroups(pass, file)
		marker := generatedMarkerOf(file)
		for _, group := range file.Comments {
			if outputs[group] {
				continue
			}
			judgeGroup(pass, group, marker)
		}
	}
	return nil, nil
}

// generatedMarkerOf is the first comment before file's package clause that is the generated-code marker, where
// go/ast.IsGenerated looks for it, or nil when file has none. The convention counts the line only there, so the same
// words after the package clause are a comment like any other and are read.
func generatedMarkerOf(file *ast.File) *ast.Comment {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if comment.Pos() > file.Package {
				return nil
			}
			if generatedMarker.MatchString(comment.Text) {
				return comment
			}
		}
	}
	return nil
}

// commentSpan is where one comment's body sits in its group's joined text.
type commentSpan struct {
	comment *ast.Comment
	start   int
	end     int
}

// judgeGroup masks a comment group as one text, then reports each comment whose own span of it still holds a shout.
// marker is the file's generated-code marker, or nil.
func judgeGroup(pass *analysis.Pass, group *ast.CommentGroup, marker *ast.Comment) {
	var joined strings.Builder
	spans := []commentSpan{}
	for _, comment := range group.List {
		// go/doc leaves a directive out of the comment's text, so it is neither read nor a line between its
		// neighbours, and the generated-code marker is set aside the same way.
		if comment == marker || directive.MatchString(comment.Text) {
			continue
		}
		if len(spans) > 0 {
			joined.WriteByte('\n')
		}
		start := joined.Len()
		joined.WriteString(bodyOf(comment))
		spans = append(spans, commentSpan{comment: comment, start: start, end: joined.Len()})
	}
	text := joined.String()
	// The shared gate: without two capitals in a row nothing can be reported, so most groups skip the masking.
	if !shouting.HasAdjacentUppercaseLetters(text) {
		return
	}
	masked := shouting.MaskCodeAndCommands(text)
	for _, span := range spans {
		tokens := shouting.TokensInMasked(masked[span.start:span.end])
		if len(tokens) == 0 {
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:      span.comment.Pos(),
			End:      span.comment.End(),
			Category: shoutingInCommentMessage.Id,
			Message:  shoutingInCommentMessage.Render(map[string]string{"tokens": shouting.QuotedTokenList(tokens)}),
		})
	}
}

// bodyOf is the text of comment the masks read. A // comment loses its slashes and the one space after them, as
// go/ast's CommentGroup.Text strips them, so a tab or the further spaces after it are indentation the indented code
// block mask can measure. A block comment is kept whole, delimiters included, as the TypeScript rule reads one, since
// the masks already know its gutter. A go/doc note marker opening the comment is blanked, keeping every offset.
func bodyOf(comment *ast.Comment) string {
	start := 0
	if strings.HasPrefix(comment.Text, "//") {
		start = len("//")
		if strings.HasPrefix(comment.Text[start:], " ") {
			start++
		}
	}
	body := comment.Text[start:]
	location := noteMarker.FindStringSubmatchIndex(comment.Text)
	if location == nil {
		return body
	}
	// The marker follows the slashes and any spaces or tabs after them, so it never starts before the body does.
	markerStart, markerEnd := location[2]-start, location[3]-start
	return body[:markerStart] + strings.Repeat(" ", markerEnd-markerStart) + body[markerEnd:]
}

// exampleOutputGroups is the comment group holding each example's expected output in file, found as go/doc finds it:
// the last comment group inside an example function's body, when its text opens with Output: or Unordered output:.
// An output comment anywhere else is only a comment, and go test does not compare against it.
func exampleOutputGroups(pass *analysis.Pass, file *ast.File) map[*ast.CommentGroup]bool {
	outputs := map[*ast.CommentGroup]bool{}
	// The file's own name, not one a //line directive gives it, since only a _test.go file holds examples.
	if !strings.HasSuffix(pass.Fset.PositionFor(file.Package, false).Filename, "_test.go") {
		return outputs
	}
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || !isExample(function) {
			continue
		}
		var last *ast.CommentGroup
		for _, group := range file.Comments {
			if group.Pos() < function.Body.Pos() {
				continue
			}
			if group.End() > function.Body.End() {
				break
			}
			last = group
		}
		if last != nil && exampleOutput.MatchString(last.Text()) {
			outputs[last] = true
		}
	}
	return outputs
}

// isExample is go/doc's reading of an example function: no receiver, a body, no parameters, and named Example, or
// Example followed by anything but a lower-case letter.
func isExample(function *ast.FuncDecl) bool {
	if function.Recv != nil || function.Body == nil || function.Type.Params.NumFields() > 0 {
		return false
	}
	rest, found := strings.CutPrefix(function.Name.Name, "Example")
	if !found {
		return false
	}
	if rest == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(first)
}
