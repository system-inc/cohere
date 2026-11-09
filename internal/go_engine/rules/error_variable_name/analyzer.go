// Package error_variable_name is staticcheck's `ST1012`, a package-level variable holding a new error whose name does
// not start with err, or Err when it is exported, ported onto the engine's syntax dispatcher.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 stylecheck/st1012/st1012.go, whose message it reports
// word for word, at the variable's name, over the name's whole range, as the original does, in generated files too,
// which the original does not filter. The original reads only the syntax of each file's top-level variable
// declarations and the type information its call helper asks, and requires nothing, so the port hears each file as
// the walk enters it and reads the same, through the kit's IsCallToAny, staticcheck's code.IsCallToAny.
//
// The original's MergeIf, MergeIfAny, tells staticcheck's runner to keep a finding any one build configuration makes,
// and the engine runs one configuration, so there is nothing for the port to merge.
package error_variable_name

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Name is the original's code, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const Name = "ST1012"

// Doc is the original's title.
const Doc = "Poorly chosen name for error variable"

// Listener is what the port hears of each package from the dispatcher: each file as the walk enters it, whose
// top-level variable declarations are all the port reads of the syntax.
var Listener = rule.Listener{
	FileLocal: true, FileEnter: checkFile}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener.
var Analyzer = dispatch.RuleAnalyzer(rule.Namespace+Name, Name, Doc, &Listener)

// checkFile judges every variable file declares at its top level with a value of its own that is a call of
// errors.New or fmt.Errorf.
func checkFile(context *rule.Context, file *ast.File) {
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			valueSpecification, isValueSpecification := specification.(*ast.ValueSpec)
			// A variable declaration holds only value specifications, so the first is never met. A specification
			// whose names and values differ in number, one call giving several values or none given, pairs no name
			// with a value of its own, and the original leaves it alone.
			if !isValueSpecification || len(valueSpecification.Names) != len(valueSpecification.Values) {
				continue
			}
			for index, name := range valueSpecification.Names {
				if !staticcheck_code.IsCallToAny(context, valueSpecification.Values[index], "errors.New",
					"fmt.Errorf") {
					continue
				}
				// net/http bundles the code of its second protocol version, whose internal error variables start
				// `http2err`, and the original leaves them to that bundle's spelling.
				if context.Package.Path() == "net/http" && strings.HasPrefix(name.Name, "http2err") {
					continue
				}
				prefix := "err"
				if name.IsExported() {
					prefix = "Err"
				}
				if !strings.HasPrefix(name.Name, prefix) {
					staticcheck_report.Report(context, name,
						fmt.Sprintf("error var %s should have name of the form %sFoo", name.Name, prefix))
				}
			}
		}
	}
}
