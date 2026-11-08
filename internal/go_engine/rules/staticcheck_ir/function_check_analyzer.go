//cohere:file-ignore consistency-require-separated-folder-words staticcheck's name, a project's own spelling
package staticcheck_ir

import (
	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// FunctionCheckAnalyzer runs check as a plain analyzer named name, over every function Functions reads, with a
// rule.Context built once per package around pass.Report. The analyzer is the rule, registered as rule.Namespace
// followed by name, so every finding check reports is already that rule's. It is the one bridge for every check of this
// shape until the engine's dispatcher calls them itself, when it goes.
func FunctionCheckAnalyzer(name string, doc string, check rule.FunctionCheck) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     name,
		Doc:      doc,
		Requires: []*analysis.Analyzer{Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			functions, readError := Functions(pass)
			if readError != nil {
				return nil, readError
			}
			context := rule.NewContext(rule.Namespace+name, pass.Pkg, pass.TypesInfo, pass.Fset,
				func(_ string, diagnostic analysis.Diagnostic) {
					pass.Report(diagnostic)
				})
			for _, function := range functions {
				check(context, function)
			}
			return nil, nil
		},
	}
}
