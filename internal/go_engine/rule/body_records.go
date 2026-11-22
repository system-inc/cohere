package rule

import (
	"fmt"

	"golang.org/x/tools/go/analysis"
)

// BodyRecords keys, in a pass's ResultOf, a host's ask that U1000 record what each function body of the pass's
// package adds to its graph (#xeafx79), true, which the engine's checker hands every pass when its
// checker.Options.BodyRecords is this key and its Options.RecordBodies is set: the records an extra build's U1000
// splices in place of walking a body the build leaves standing, which the host's side of the package hands it. Only a
// host's run whose extra builds leave bodies standing asks. Every other run, a plain one among them, holds no ask, and
// U1000 records nothing there. It lives beside FactHost, the side the records are handed back on.
//
// It is a key and never runs, as Dependents is: no analyzer requires it, so it is in no graph. One that required it
// would fail on every package, naming it.
var BodyRecords = &analysis.Analyzer{
	Name: "body_records",
	Doc:  "keys a host's ask that U1000 record each function body it walks, which the checker hands it, and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("an analyzer on %s requires %s, which is the key of the ask the checker hands every "+
			"pass to record its bodies, never an analyzer to run", pass.Pkg.Path(), pass.Analyzer.Name)
	},
}
