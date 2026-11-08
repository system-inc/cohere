package dispatch

import (
	"fmt"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// RuleAnalyzer registers listener on dispatcher as the rule named rule.Namespace followed by name, and returns the
// rule's own analyzer, named name, documented by doc, which requires the dispatcher. Its run reports each finding
// the dispatcher's listener made on the package, or returns the listener's crash there, so a finding and a crash are
// the rule's own, as they were when the rule walked the package itself. Its options are flags the caller adds to the
// analyzer, which the listener's context reads.
func RuleAnalyzer(name string, doc string, listener *rule.Listener, dispatcher *Ir) *analysis.Analyzer {
	ruleName := rule.Namespace + name
	analyzer := &analysis.Analyzer{
		Name:     name,
		Doc:      doc,
		Requires: []*analysis.Analyzer{dispatcher.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			result, isResult := pass.ResultOf[dispatcher.Analyzer].(Result)
			if !isResult {
				return nil, fmt.Errorf("reading the dispatcher's result for %s: it is %T, where a dispatch.Result "+
					"was expected", ruleName, pass.ResultOf[dispatcher.Analyzer])
			}
			outcome, found := result[ruleName]
			if !found {
				return nil, fmt.Errorf("the dispatcher holds no outcome for %s, so its listener was never "+
					"registered on the dispatcher the rule requires", ruleName)
			}
			if outcome.Crash != nil {
				return nil, outcome.Crash
			}
			for _, diagnostic := range outcome.Diagnostics {
				pass.Report(diagnostic)
			}
			return nil, nil
		},
	}
	dispatcher.register(ruleName, analyzer, listener)
	return analyzer
}
