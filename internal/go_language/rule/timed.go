package rule

import (
	"fmt"

	"golang.org/x/tools/go/analysis"
)

// Timed keys, in a pass's ResultOf, the run's *timing.Collector under `--timing`, which the engine's checker hands
// every pass when its checker.Options.Timed is this key and its Options.Timing is set (#3apgjer). The checker measures
// each analyzer's run itself; a dispatcher reads the collector here to bill each listener's calls to its rule rather
// than to its own walk. It lives beside Dependents, for Dependents' reasons.
//
// It is a key and never runs, as Dependents is: no analyzer requires it, so it is in no graph. One that required it
// would fail on every package, naming it.
var Timed = &analysis.Analyzer{
	Name: "timed",
	Doc:  "keys the collector a run under --timing measures into, and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("an analyzer on %s requires %s, which is the key of the collector the checker hands "+
			"every pass under --timing, never an analyzer to run", pass.Pkg.Path(), pass.Analyzer.Name)
	},
}
