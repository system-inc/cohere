package standing_facts

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
)

// pushedReasons are why a function was named for a pushed fact of the analyzer each is keyed by, which the build's
// fact differs from the host's in (#5vbs1np, must 2).
var pushedReasons = map[string]fact_reach.Reason{
	printf.Analyzer.Name: "printf wrapper",
}

// pushedFacts are the build's own pushed facts the pass reads, beside ctrlflow's no-return: printf's wrapper facts,
// its result on the package, which holds the facts of the package's own functions and those its imports exported
// that its pass looked up, as the host's capture holds them (check.FactReachPackage.Pushed).
type pushedFacts struct {
	wrappers *printf.Wrappers
}

// words is the build's fact of function learned by the analyzer named analyzer, in the fact's words as the host's
// capture writes them (fmt.Sprint), empty for no fact, and false where the build has nothing to compare: an analyzer
// whose facts the pass does not read, or a function printf's pass never looked a fact up of, which no finding of the
// package reads the fact of.
func (pushed pushedFacts) words(analyzer string, function *types.Func) (string, bool) {
	if analyzer == printf.Analyzer.Name {
		return pushed.wrappers.Of(function)
	}
	return "", false
}
