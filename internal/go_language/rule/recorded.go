package rule

import (
	"fmt"

	"golang.org/x/tools/go/analysis"
)

// Recorded keys, in a pass's ResultOf, the *build_reads.Reads of the pass's package, which the engine's checker hands
// every pass when its checker.Options.Recorded is this key and its Options.Reads is set (#b2t9xmf), nil for a package
// the run records no reads of. A rule that asks another package something past its own syntax, a name in another
// package's scope, an interface of its import closure, whether an object is deprecated, records the question and its
// answer there, through Context.Reads, so an extra build restores the package only while every answer
// holds. It lives beside Dependents, for Dependents' reasons.
//
// It is a key and never runs, as Dependents is: no analyzer requires it, so it is in no graph. One that required it
// would fail on every package, naming it.
var Recorded = &analysis.Analyzer{
	Name: "build_reads",
	Doc:  "keys the reads of the pass's package that a rule records its lookups of other packages in, and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("an analyzer on %s requires %s, which is the key of the reads the checker hands every "+
			"pass, never an analyzer to run", pass.Pkg.Path(), pass.Analyzer.Name)
	},
}
