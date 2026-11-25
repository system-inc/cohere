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
// only its own words, each at the word. Go's own notations are not prose and are not read: directives, go/doc notes,
// an example's output, and the marker a generated file opens with.
package consistency_no_shouting

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/rule"
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
// words after the package clause are a comment like any other and are read. What the marker is, its capitals the
// convention's and no writer's, is shouting.IsGeneratedMarker's one judgment, which the TypeScript rule reads too.
func generatedMarkerOf(file *ast.File) *ast.Comment {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if comment.Pos() > file.Package {
				return nil
			}
			if shouting.IsGeneratedMarker(comment.Text) {
				return comment
			}
		}
	}
	return nil
}

// commentSpan is where one comment's body sits in its group's joined text, and where it starts in the comment's own
// text.
type commentSpan struct {
	comment *ast.Comment
	start   int
	end     int
	// bodyStart is what bodyOf cut from the front of the comment's text, so a word's offset in the body plus this is
	// its offset in the comment.
	bodyStart int
}

// judgeGroup masks a comment group as one text, then reports each shout left in each comment's own span of it, one
// finding per word covering the word's bytes, so a fix lands on what shouted rather than the whole comment. marker is
// the file's generated-code marker, or nil.
func judgeGroup(pass *analysis.Pass, group *ast.CommentGroup, marker *ast.Comment) {
	if !anyReadCommentShouts(group, marker) {
		return
	}
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
		body, bodyStart := bodyOf(comment)
		joined.WriteString(body)
		spans = append(spans, commentSpan{comment: comment, start: start, end: joined.Len(), bodyStart: bodyStart})
	}
	text := joined.String()
	// Asked again of the joined text, since a comment the gate admitted for its go/doc note marker alone holds no
	// shout once the marker is blanked, and masking costs more than the asking.
	if !shouting.HasShoutedToken(text) {
		return
	}
	masked := shouting.MaskCodeAndCommands(text)
	for _, span := range spans {
		// Masking keeps every byte's place, so a word's offset in the span is its offset in the body, and the text's
		// bytes are the file's: go/scanner changes a comment's text only by dropping a carriage return, which gofmt
		// never writes.
		for _, shouted := range shouting.ShoutedTokensInMasked(masked[span.start:span.end]) {
			start := span.comment.Pos() + token.Pos(span.bodyStart+shouted.Offset)
			pass.Report(analysis.Diagnostic{
				Pos:      start,
				End:      start + token.Pos(len(shouted.Text)),
				Category: shoutingInCommentMessage.Id,
				Message: shoutingInCommentMessage.Render(map[string]string{
					"tokens": shouting.QuotedTokenList([]string{shouted.Text}),
				}),
			})
		}
	}
}

// anyReadCommentShouts is the gate asked of each comment's own text before judgeGroup joins the group, so a group
// whose capitals are all acronyms, or sit only in a directive or the generated-code marker, allocates nothing. Two
// capitals in a row are asked first, being the cheaper scan, and the directive and the marker are passed over as
// judgeGroup passes over them.
//
// It cannot skip a finding. A finding is a shouted token of the masked joined text, so by shouting.HasShoutedToken's
// argument a shouted token of the joined text, and a token holds no newline, so it lies in one comment's body. That
// body is the comment's text with only its delimiters cut and a go/doc note marker blanked, and every byte beside
// what was cut or blanked, a slash, a star, a space or the marker's closing parenthesis, is neither a word character
// nor a hyphen or ampersand that joins a run. So a token, and the run it is judged in, reads the same in the text as
// in the body, and the comment's text holds it. The marker's own capitals can only admit a group, never hide one.
func anyReadCommentShouts(group *ast.CommentGroup, marker *ast.Comment) bool {
	for _, comment := range group.List {
		if comment == marker || !shouting.HasAdjacentUppercaseLetters(comment.Text) {
			continue
		}
		if !directive.MatchString(comment.Text) && shouting.HasShoutedToken(comment.Text) {
			return true
		}
	}
	return false
}

// bodyOf is the text of comment the masks read, and where it starts in the comment's text, so a word maps back to the
// file. A // line is shouting.GoCommentBody, which the warning-comment rule reads too, so the two rules measure a code
// block's indentation alike. A block comment loses its `/*` and `*/` (#0n6zmqq), as the TypeScript rule and ESLint's
// comment value read one: the command patterns anchor at a line's start, so a `/* git commit --amend */` read whole
// opens on its delimiter and is never masked. The delimiters are cut, not blanked, since two spaces before a line's
// text would make it an indented code block. A go/doc note marker opening the comment is blanked, keeping every
// offset.
func bodyOf(comment *ast.Comment) (string, int) {
	body := shouting.GoCommentBody(comment.Text)
	// A // line's body is a suffix of its text, so what it lost from the front is where it starts.
	start := len(comment.Text) - len(body)
	if afterOpening, isBlock := strings.CutPrefix(comment.Text, "/*"); isBlock {
		// go/scanner reports an unterminated block comment as an error and the load stops, but the cut is safe
		// without its closer anyway.
		body = strings.TrimSuffix(afterOpening, "*/")
		start = len("/*")
	}
	location := noteMarker.FindStringSubmatchIndex(comment.Text)
	if location == nil {
		return body, start
	}
	// The marker follows the delimiter and any spaces or tabs after it, so it never starts before the body does.
	markerStart, markerEnd := location[2]-start, location[3]-start
	return body[:markerStart] + strings.Repeat(" ", markerEnd-markerStart) + body[markerEnd:], start
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
