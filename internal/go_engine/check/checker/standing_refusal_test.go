package checker

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// refusalAnalyzer is an analyzer named name that requires required and reports nothing.
func refusalAnalyzer(name string, required ...*analysis.Analyzer) *analysis.Analyzer {
	return &analysis.Analyzer{Name: name, Doc: "requires what it is given and reports nothing", Requires: required,
		Run: func(*analysis.Pass) (any, error) { return nil, nil }}
}

// Analyze refuses an Options.StandingAfter that is a dispatcher, or that requires one directly or through what it
// requires, with an error naming it and the dispatcher, since the edge it adds would have each dispatcher's action
// wait on itself (#e4rd5dw). An analyzer that requires no dispatcher is taken, and the graph analyzed.
func TestAnalyzeRefusesAStandingAfterThatRequiresADispatcher(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	dispatcher := refusalAnalyzer("dispatcher")
	tolerant := func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher }
	options := func(after *analysis.Analyzer) *Options {
		return &Options{Standing: standingKey, Tolerant: tolerant, StandingAfter: after,
			StandingBodies: func(string, any) rule.StandingBodies { return nil }}
	}
	middle := refusalAnalyzer("middle", dispatcher)
	for _, refused := range []*analysis.Analyzer{
		dispatcher,
		refusalAnalyzer("direct", dispatcher),
		refusalAnalyzer("through", middle),
	} {
		_, analyzeError := Analyze([]*analysis.Analyzer{dispatcher}, roots, options(refused))
		if analyzeError == nil || !strings.Contains(analyzeError.Error(), refused.Name+", requires the dispatcher "+
			"dispatcher") {
			test.Errorf("a StandingAfter %s was analyzed with the error %v, want it refused, naming it and the "+
				"dispatcher", refused.Name, analyzeError)
		}
	}
	if _, analyzeError := Analyze([]*analysis.Analyzer{dispatcher}, roots,
		options(refusalAnalyzer("free"))); analyzeError != nil {
		test.Errorf("a StandingAfter that requires no dispatcher was refused: %v", analyzeError)
	}
}
