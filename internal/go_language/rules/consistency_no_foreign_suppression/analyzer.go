// Package consistency_no_foreign_suppression is cohere-go's rule that a suppression in our code is cohere's own.
//
// `//cohere:ignore <rule> <reason>` is the one way to silence a finding, because cohere holds it to two checks: a
// directive with no reason suppresses nothing and is itself a finding, and one that withholds nothing is
// `consistency-no-unused-ignore`. Another tool's directive passes neither. staticcheck's `//lint:ignore` and
// `//lint:file-ignore` are honored by the U1000 port, which treats what they annotate as used, so they keep code alive
// on a reason nothing reads and outlive the finding they hid. golangci-lint's `//nolint` is honored by nothing here,
// so it reads as a suppression to the next reader while it silences nothing. Either way the reasoning goes unchecked.
//
// A directive is judged the way its tool reads one. staticcheck reads a `//` comment whose text starts `//lint:`, any
// command after it included, so a malformed `//lint:IGNORE` is still its directive. golangci-lint reads `nolint`
// followed by a colon, a space or nothing, and its machine-readable form, the one Go's directive grammar describes
// (go.dev/doc/comment), has no space after the slashes, so `//nolint`, `//nolint:name` and `//nolint // why` are
// directives wherever the comment sits, on its own line or trailing code. A comment that opens with a space, as in
// "// nolint is ...", is prose, since a sentence may start with the word, and so is a mention of the directives
// later in a comment. Comments are all the rule reads: a string literal is never a comment, so a test's raw-string
// fixture holding `//lint:ignore` is left alone, and so is a block comment.
//
// A house rule judges our code alone: in a generated file or one marked `cohere:copied`, the driver withholds this
// rule's findings as it does every house rule's, so an upstream copy may keep its upstream's directives.
package consistency_no_foreign_suppression

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-foreign-suppression"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_foreign_suppression",
	Doc: "Flags staticcheck's //lint: directives and golangci-lint's //nolint in a comment, because cohere checks " +
		"neither their reason nor whether they still silence anything. //cohere:ignore <rule> <reason> is the one " +
		"suppression. A mention of either later in a comment, and text in a string literal, are not read.",
	Run: run,
}

// Rule is this rule's entry in cohere-go's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var foreignSuppressionMessage = policy.MessageOf(Name, "foreignSuppression")

const (
	// staticcheckPrefix opens every staticcheck directive, as lint.ParseDirectives tests each comment's text.
	staticcheckPrefix = "//lint:"
	// golangciPrefix opens golangci-lint's directive, which a colon, a space or the comment's end must follow.
	golangciPrefix = "//nolint"
)

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				judgeComment(pass, comment)
			}
		}
	}
	return nil, nil
}

// judgeComment reports comment when it is another tool's suppression, over the directive's first word, the part
// naming the tool and the checks it silences.
func judgeComment(pass *analysis.Pass, comment *ast.Comment) {
	tool := foreignTool(comment.Text)
	if tool == "" {
		return
	}
	directive, _, _ := strings.Cut(comment.Text, " ")
	pass.Report(analysis.Diagnostic{
		Pos:      comment.Pos(),
		End:      comment.Pos() + token.Pos(len(directive)),
		Category: foreignSuppressionMessage.Id,
		Message: foreignSuppressionMessage.Render(map[string]string{
			"directive": directive,
			"tool":      tool,
		}),
	})
}

// foreignTool is the tool whose suppression text is, or empty when text is no other tool's directive.
func foreignTool(text string) string {
	if strings.HasPrefix(text, staticcheckPrefix) {
		return "staticcheck"
	}
	rest, isGolangci := strings.CutPrefix(text, golangciPrefix)
	if isGolangci && (rest == "" || rest[0] == ':' || rest[0] == ' ') {
		return "golangci-lint"
	}
	return ""
}
