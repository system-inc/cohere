package rule

import (
	"fmt"

	"golang.org/x/tools/go/analysis"
)

// Dependents keys, in a pass's ResultOf, the set of analyzers whose actions on the pass's package require the pass's
// own, a map[*analysis.Analyzer]bool, which the engine's checker gathers for each graph when its
// checker.Options.Dependents is this key. A dispatcher reads it to call only the listeners of the rules the run
// enabled, since a listener joins its dispatcher as its package initializes, whether or not a run enables its rule.
// It lives here because the engine's check driver and the dispatchers both import this package already, so the key
// adds an import to neither, and a rule package that registers on a dispatcher depends on nothing new.
//
// It is a key and never runs: no analyzer requires it, so it is in no graph. One that required it would fail on
// every package, naming it.
var Dependents = &analysis.Analyzer{
	Name: "dependents",
	Doc:  "keys the set of analyzers that require a pass's analyzer on its package, and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("an analyzer on %s requires %s, which is the key of a set the checker hands every "+
			"pass, never an analyzer to run", pass.Pkg.Path(), pass.Analyzer.Name)
	},
}
