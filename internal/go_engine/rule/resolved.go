package rule

import (
	"fmt"

	"golang.org/x/tools/go/analysis"
)

// Resolved keys, in a pass's ResultOf, the run's fact resolver, a *fact_resolver.Resolver, which the engine's checker
// hands every pass when its checker.Options.Resolved is this key and its Options.Resolver is set (#w7zmvat). A reader
// of a fact computed on demand reads it through fact_resolver.Of; build_intermediate_representation reads it to take
// the package the resolver shares with it, so a root's representation is built once in a run (#wt5vaft). It lives
// beside Dependents, for Dependents' reasons: build_intermediate_representation, which the resolver imports, reads it
// without importing the resolver back.
//
// It is a key and never runs, as Dependents is: no analyzer requires it, so it is in no graph. One that required it
// would fail on every package, naming it.
var Resolved = &analysis.Analyzer{
	Name: "fact_resolver",
	Doc:  "keys the resolver that computes facts on demand, which the checker hands every pass, and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("an analyzer on %s requires %s, which is the key of the resolver the checker hands "+
			"every pass, never an analyzer to run", pass.Pkg.Path(), pass.Analyzer.Name)
	},
}
