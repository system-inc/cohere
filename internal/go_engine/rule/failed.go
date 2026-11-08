package rule

import (
	"fmt"

	"golang.org/x/tools/go/analysis"
)

// Failed keys, in a pass's ResultOf, the prerequisites that failed on the pass's package and that the pass runs past,
// a map[*analysis.Analyzer]error from each failed analyzer to its error, which the engine's checker hands an action it
// tolerates when its checker.Options.Failed is this key. A dispatcher is such an action: it requires every one of its
// listeners' Requires, so one listener's requirement failing on a package would otherwise fail the dispatcher there,
// and every rule on it. With the map, the dispatcher starts every listener but those whose own requirements failed.
// It lives beside Dependents, for Dependents' reasons.
//
// It is a key and never runs, as Dependents is: no analyzer requires it, so it is in no graph. One that required it
// would fail on every package, naming it.
var Failed = &analysis.Analyzer{
	Name: "failed",
	Doc:  "keys the prerequisites that failed on a pass's package and that the pass runs past, and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("an analyzer on %s requires %s, which is the key of a map the checker hands a pass "+
			"that runs past a failed prerequisite, never an analyzer to run", pass.Pkg.Path(), pass.Analyzer.Name)
	},
}
