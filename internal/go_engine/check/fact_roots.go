package check

import (
	"errors"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// factRootsOf is where an extra build roots the analyzers that learn facts apart from its rules (checker.Options
// FactRoots), and which analyzers those are: the session's known.facts, every analyzer that learns facts among the
// rules and what they require, on every package of the load below analyzed that the build checks from source, that no
// wave restores (restoredInWave, which a package restored from the host's run is too), and that toAnalyze leaves out,
// in the load's order (#xqbqcbt).
//
// Such a package is no root of the build, yet the decide asks its facts from source: a package restored from the
// host's run that read a fact of it is held only when the build's own facts there read alike, and with no action of a
// fact's type on it the build has none to ask, so the restore is declined and the build loaded again
// (buildFacts.factsHold). A package given as its test variant, `p [p.test]`, leaves plain p such a dependency, imported
// by packages the build restores from the host. Each used to be reached by the chain of fact analyzers a root on a host
// restore seeded over what it imports; with those roots gone, nothing else reaches it. A package the build was given
// runs every one of them already through its rules' requirements, and one restored gives its facts from its entry.
//
// The host's run, and any run restoring nothing from a host's, has none: a dependency it checks from source is reached
// by the chain of a root it analyzes, and nothing else asks its facts.
func factRootsOf(
	analyzed []*packages.Package,
	toAnalyze []*packages.Package,
	session *CacheSession,
) ([]*packages.Package, []*analysis.Analyzer) {
	if session.host == nil || session.known == nil || len(session.known.facts) == 0 {
		return nil, nil
	}
	rooted := make(map[*packages.Package]bool, len(toAnalyze))
	for _, given := range toAnalyze {
		rooted[given] = true
	}
	var factRoots []*packages.Package
	packages.Visit(analyzed, nil, func(visited *packages.Package) {
		if !rooted[visited] && !session.restoredInWave(visited) {
			factRoots = append(factRoots, visited)
		}
	})
	if len(factRoots) == 0 {
		return nil, nil
	}
	return factRoots, session.known.facts
}

// restoringFromHostRefuses is why a run cannot restore from the host's run as options ask, nil when it can. A package
// restored from the host's run has no action unless a pass above it reaches it (factRootsOf), so two things an action
// on it once did must hold without one:
//   - The resolver must know it by its types before a question reaches it. Run tells the session of every package the
//     load reached before the analysis, but beside the load (Options.Ready) a package is told only as an action on it
//     restores it, which a host restore may never have. Only the host's speculative pass runs beside its load, and the
//     host restores from no host, so an extra build there is a run no caller makes.
//   - The facts census (Options.ExportFacts) reads each package's facts off the actions on it, and a host restore with
//     none would be missing from it. A run judging each build's dependence keeps the restore from the host off
//     (censusKeepsHostRestoreOff), so no extra build there restores from the host either.
//
// Either one, asked anyway, would read wrong rather than fail, so Run refuses it.
func restoringFromHostRefuses(options Options) error {
	if options.Cache == nil || options.Cache.host == nil {
		return nil
	}
	if options.Ready != nil {
		return errors.New("running the rules beside the load: an extra build restoring from the host's run gives a " +
			"host restore no action, and beside the load only an action on a restored package tells the resolver of " +
			"it, so a question reaching one would find it unknown")
	}
	if options.ExportFacts {
		return errors.New("exporting the facts for the census: an extra build restoring from the host's run gives a " +
			"host restore no action, and the census reads each package's facts off the actions on it, so a host " +
			"restore's facts would be missing")
	}
	return nil
}
