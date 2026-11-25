package rule

import (
	"fmt"

	"golang.org/x/tools/go/analysis"
)

// FactHost keys, in a pass's ResultOf, the host's side of the pass's package for the facts' reach, a fact_reach.Host,
// which cohere-go's checker hands every pass on a package when its checker.Options.FactHost is this key and its
// Options.FactHosts answers one for the package (#5vbs1np): what the host's run held of the package's facts and
// answers, which an extra build's pass compares with its own before any rule runs there, so a function body the build
// leaves to the host's findings is one whose every fact and answer reads the same in both (standing_facts). A package
// the build lints whole, or a run that is no extra build, holds none. It lives beside Resolved, for Resolved's reasons.
//
// It is a key and never runs, as Dependents is: no analyzer requires it, so it is in no graph. One that required it
// would fail on every package, naming it.
var FactHost = &analysis.Analyzer{
	Name: "fact_host",
	Doc:  "keys the host's side of the pass's package for the facts' reach, which the checker hands it, and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("an analyzer on %s requires %s, which is the key of the host's side the checker hands "+
			"a package's passes, never an analyzer to run", pass.Pkg.Path(), pass.Analyzer.Name)
	},
}
