package checker

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// standingKey stands in for the engine's key, rule.Standing, an analyzer that is never run.
var standingKey = &analysis.Analyzer{
	Name: "standing_key",
	Doc:  "keys a package's standing bodies and never runs",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, fmt.Errorf("the key ran on %s", pass.Pkg.Path())
	},
}

// standingOf is the standing bodies answered for one package: the ID it was answered for, and what it was asked with.
type standingOf struct {
	packageId string
	after     string
}

// Stands answers that nothing stands.
func (standingOf) Stands(string, int) bool { return false }

// Confines answers that no rule is confined.
func (standingOf) Confines(string) bool { return false }

// Findings answers that the host holds no findings.
func (standingOf) Findings(string, string) ([]rule.InheritedFinding, bool) { return nil, false }

// Took records nothing.
func (standingOf) Took(string, string, int, int) {
	// The checker's test reads what a pass holds, never a take.
}

// standingOfPackage is an Options.StandingOf that answers each package with its own ID and what it was asked with,
// which must be nothing or a string, and counts its calls into asked.
func standingOfPackage(asked *sync.Map) func(*packages.Package, any) rule.StandingBodies {
	return func(analyzed *packages.Package, after any) rule.StandingBodies {
		asked.Store(analyzed.ID, true)
		afterText, _ := after.(string)
		return standingOf{packageId: analyzed.ID, after: afterText}
	}
}

// analyzeStanding runs analyzers over roots under options and fails test on any action's error but the followed
// analyzer's, returning how many actions the graph holds.
func analyzeStanding(
	test *testing.T,
	analyzers []*analysis.Analyzer,
	roots []*packages.Package,
	options *Options,
) int {
	test.Helper()
	graph, analyzeError := Analyze(analyzers, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	actions := 0
	for action := range graph.All() {
		actions++
		if action.Err != nil && action.Analyzer.Name != "followed" {
			test.Fatalf("%s failed: %v", action, action.Err)
		}
	}
	return actions
}

// Under Options.StandingOf a dispatcher's pass on a package, one of an analyzer Options.Tolerant names, holds under
// Options.Standing what the option answers for that very package, asked with nothing while Options.StandingAfter is
// unset. A pass of any other analyzer holds nothing there, and neither does a dispatcher's on a package the option
// answers nil for, no nil under the key either. Without the option no pass holds anything under the key, and the
// graph holds the same actions either way.
func TestADispatchersPassHoldsItsOwnPackagesStandingBodies(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	// The first package is the one nothing stands in.
	bare := roots[0].ID
	passes := newStandingPasses()
	dispatcher, plain := passes.analyzer("dispatcher"), passes.analyzer("plain")
	isDispatcher := func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher }
	actions := analyzeStanding(test, []*analysis.Analyzer{dispatcher, plain}, roots, &Options{
		Tolerant: isDispatcher,
		Standing: standingKey,
		StandingOf: func(analyzed *packages.Package, after any) rule.StandingBodies {
			if analyzed.ID == bare || after != nil {
				return nil
			}
			return standingOf{packageId: analyzed.ID}
		},
	})
	for _, root := range roots {
		held := passes.passes["dispatcher "+root.PkgPath].standing
		if root.ID == bare {
			if passes.passes["dispatcher "+root.PkgPath].keyed {
				test.Errorf("the dispatcher's pass on %s, which the option answers nil for, holds %v under Standing, "+
					"want nothing there, no nil either", root.ID, held)
			}
		} else if held != (standingOf{packageId: root.ID}) {
			test.Errorf("the dispatcher's pass on %s holds %v under Standing, want what the option answers for its "+
				"own package, asked with nothing", root.ID, held)
		}
		if plain := passes.passes["plain "+root.PkgPath]; plain.keyed {
			test.Errorf("the pass of an analyzer that is no dispatcher holds %v under Standing on %s", plain.standing,
				root.ID)
		}
	}

	unset := newStandingPasses()
	unsetDispatcher := unset.analyzer("dispatcher")
	unsetActions := analyzeStanding(test, []*analysis.Analyzer{unsetDispatcher, unset.analyzer("plain")}, roots,
		&Options{Tolerant: func(analyzer *analysis.Analyzer) bool { return analyzer == unsetDispatcher },
			Standing: standingKey})
	for key, found := range unset.passes {
		if found.keyed {
			test.Errorf("without the option the pass %s holds %v under Standing", key, found.standing)
		}
	}
	if actions != 2*len(roots) || unsetActions != actions {
		test.Errorf("the graph holds %d actions with the option and %d without, want one for each of two analyzers on "+
			"each of %d packages both ways", actions, unsetActions, len(roots))
	}
}

// Under Options.StandingAfter each dispatcher's action on a package follows that analyzer's action there, though the
// dispatcher does not require it: the followed analyzer has finished on the package before the dispatcher's pass
// runs, Options.StandingOf is asked with its result there, and the result is no input of the dispatcher's pass. An
// analyzer that is no dispatcher follows nothing. A dispatcher that requires the analyzer itself follows it by its own
// edge, holds its result as any input, and the option is asked with it all the same. One that requires it only for a
// listener no rule of the run has, an edge Options.ForDependents drops, follows it by the edge Analyze adds once that
// one is dropped, so it still follows, and holds no result of it.
func TestADispatcherFollowsTheStandingAnalyzerAndTheOptionIsAskedWithItsResult(test *testing.T) {
	test.Parallel()
	for _, requirement := range []struct {
		name     string
		requires bool
		dropped  bool
		results  []string
	}{
		{name: "the dispatcher does not require it", results: []string{}},
		{name: "the dispatcher requires it", requires: true, results: []string{"followed"}},
		{name: "the dispatcher's requirement is dropped", requires: true, dropped: true, results: []string{}},
	} {
		test.Run(requirement.name, func(test *testing.T) {
			test.Parallel()
			roots := timedModule(test)
			passes := newStandingPasses()
			followed := passes.followedAnalyzer(nil)
			dispatcher := passes.analyzer("dispatcher")
			if requirement.requires {
				dispatcher = passes.analyzer("dispatcher", followed)
			}
			results := requirement.results
			asked := &sync.Map{}
			options := &Options{
				Tolerant:      func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher },
				Standing:      standingKey,
				StandingOf:    standingOfPackage(asked),
				StandingAfter: followed,
			}
			if requirement.dropped {
				options.ForDependents = func(analyzer *analysis.Analyzer, required *analysis.Analyzer) bool {
					return analyzer == dispatcher && required == followed
				}
			}
			actions := analyzeStanding(test, []*analysis.Analyzer{dispatcher, passes.analyzer("plain")}, roots,
				options)
			for _, root := range roots {
				found := passes.passes["dispatcher "+root.PkgPath]
				expected := standingOf{packageId: root.ID, after: "followed on " + root.PkgPath}
				if !found.followed || found.standing != expected || !slices.Equal(found.results, results) {
					test.Errorf("the dispatcher's pass on %s ran after the followed analyzer: %t, holds %v under "+
						"Standing and the results %v, want it after, holding %v and the results %v", root.ID,
						found.followed, found.standing, found.results, expected, results)
				}
				if plain := passes.passes["plain "+root.PkgPath]; plain.standing != nil || len(plain.results) != 0 {
					test.Errorf("the pass of an analyzer that is no dispatcher holds %v under Standing and the "+
						"results %v on %s, want neither", plain.standing, plain.results, root.ID)
				}
			}
			if actions != 3*len(roots) {
				test.Errorf("the graph holds %d actions, want the dispatcher's, the other analyzer's and the "+
					"followed one's on each of %d packages", actions, len(roots))
			}
		})
	}
}

// Where the analyzer Options.StandingAfter names failed on a package, the dispatcher there runs all the same and
// holds nothing under Options.Standing, the option never asked: every doubt lints. The failure of the edge Analyze
// added is no part of what the dispatcher reads under Options.Failed, and it fails no dispatcher even where
// Options.Failed is unset, when an action runs past nothing else.
func TestAFailedStandingAnalyzerLeavesNothingStandingAndFailsNoDispatcher(test *testing.T) {
	test.Parallel()
	for _, failed := range []*analysis.Analyzer{failedKey, nil} {
		test.Run(fmt.Sprintf("Failed set: %t", failed != nil), func(test *testing.T) {
			test.Parallel()
			roots := timedModule(test)
			passes := newStandingPasses()
			followed := passes.followedAnalyzer(errors.New("planted failure"))
			dispatcher := passes.analyzer("dispatcher")
			asked := &sync.Map{}
			analyzeStanding(test, []*analysis.Analyzer{dispatcher}, roots, &Options{
				Failed:        failed,
				Tolerant:      func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher },
				Standing:      standingKey,
				StandingOf:    standingOfPackage(asked),
				StandingAfter: followed,
			})
			for _, root := range roots {
				found, ran := passes.passes["dispatcher "+root.PkgPath]
				_, wasAsked := asked.Load(root.ID)
				if !ran || !found.followed || found.standing != nil || len(found.failed) != 0 || wasAsked {
					test.Errorf("on %s, where the followed analyzer failed, the dispatcher ran: %t, after it: %t, "+
						"holding %v under Standing and %v under Failed, the option asked: %t, want it run after, "+
						"holding nothing under either, and the option never asked", root.ID, ran, found.followed,
						found.standing, found.failed, wasAsked)
				}
			}
		})
	}
}

// A restored package runs nothing, so a dispatcher's action on one follows nothing: with every package restored, the
// graph holds only the dispatcher's actions and the followed analyzer never runs.
func TestADispatcherOnARestoredPackageFollowsNothing(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	passes := newStandingPasses()
	dispatcher := passes.analyzer("dispatcher")
	asked := &sync.Map{}
	actions := analyzeStanding(test, []*analysis.Analyzer{dispatcher}, roots, &Options{
		Tolerant:      func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher },
		Standing:      standingKey,
		StandingOf:    standingOfPackage(asked),
		StandingAfter: passes.followedAnalyzer(nil),
		Restored:      func(*packages.Package) bool { return true },
		Restore:       func(*Action) error { return nil },
	})
	if actions != len(roots) || len(passes.finished) != 0 || len(passes.passes) != 0 {
		test.Errorf("with every package restored the graph holds %d actions, the followed analyzer ran on %v and %d "+
			"passes ran, want only the dispatcher's action on each of %d packages and nothing run", actions,
			passes.finished, len(passes.passes), len(roots))
	}
}

// With Options.StandingOf unset, Options.StandingAfter adds no action: the analyzer it names never runs, and the
// graph holds only the actions of the analyzers asked for.
func TestWithoutTheStandingOptionNothingIsFollowed(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	passes := newStandingPasses()
	dispatcher := passes.analyzer("dispatcher")
	actions := analyzeStanding(test, []*analysis.Analyzer{dispatcher}, roots, &Options{
		Tolerant:      func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher },
		Standing:      standingKey,
		StandingAfter: passes.followedAnalyzer(nil),
	})
	if actions != len(roots) || len(passes.finished) != 0 {
		test.Errorf("the graph holds %d actions and the followed analyzer ran on %v, want only the dispatcher's on "+
			"each of %d packages and the followed analyzer never run", actions, passes.finished, len(roots))
	}
	for key, found := range passes.passes {
		if found.keyed || len(found.results) != 0 {
			test.Errorf("the pass %s holds %v under Standing and the results %v, want neither", key, found.standing,
				found.results)
		}
	}
}
