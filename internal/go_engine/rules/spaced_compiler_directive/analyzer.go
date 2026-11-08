// Package spaced_compiler_directive is staticcheck's SA9009, a line comment at the start of a line that reads as a
// compiler directive but for a space or tab after its `//`, which the compiler then ignores, ported as a check on
// syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa9009/sa9009.go, whose logic it keeps and
// whose message it reports word for word, over the whole comment, where staticcheck's report puts it. It reads the
// comments of every file of the package, as the original does, and reports through staticcheck_report.Report where
// the original reports through report.Report, the port of the one the original calls, proven against it beside it.
// The registry still runs the original, which stays the oracle: the parity tests beside this file run both over the
// same packages, and the two differ nowhere they hold.
package spaced_compiler_directive

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every file, as the walk enters it.
var Listener = rule.Listener{FileEnter: CheckFile}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA9009",
	"SA9009",
	"Flags a compiler directive the compiler ignores for a space after its slashes: staticcheck's SA9009, "+
		"ported as a check on syntax.",
	&Listener,
)

// CheckFile judges every comment of one file: a line comment that starts its line, whose text after the slashes
// opens with spaces or tabs and then `go:` and a lowercase letter, is a directive the compiler does not read, since a
// directive has its `go:` straight after the slashes.
func CheckFile(context *rule.Context, file *ast.File) {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			// A directive is a line comment, never a block comment.
			body, isLineComment := strings.CutPrefix(comment.Text, "//")
			if !isLineComment {
				continue
			}
			// A directive starts its line. A comment after code, such as `import _ "unsafe" // go:linkname`, is
			// prose. The column is the file's own, not one a line directive moves it to.
			if context.Fset.PositionFor(comment.Pos(), false).Column != 1 {
				continue
			}
			directive := strings.TrimLeft(body, " \t")
			// No space or tab after the slashes, so the comment is a directive the compiler reads, or prose.
			if len(directive) == len(body) {
				continue
			}
			name, isDirective := strings.CutPrefix(directive, "go:")
			if !isDirective {
				continue
			}
			// Only a lowercase letter after `go:` reads as a directive's name; `// go: is fun` is prose.
			if len(name) == 0 || name[0] < 'a' || name[0] > 'z' {
				continue
			}
			staticcheck_report.Report(context, comment,
				fmt.Sprintf("ineffectual compiler directive due to extraneous space: %q", comment.Text))
		}
	}
}
