package checker

import (
	"errors"
	"fmt"
	"slices"
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
// Each answer is one of its own, so two passes hold the same answer only when they hold the same one.
type standingOf struct {
	packageId string
	after     any
}

// Stands answers that nothing stands.
func (*standingOf) Stands(string, int) bool { return false }

// Confines answers that no rule is confined.
func (*standingOf) Confines(string) bool { return false }

// Findings answers that the host holds no findings.
func (*standingOf) Findings(string, string) ([]rule.InheritedFinding, bool) { return nil, false }

// Took records nothing.
func (*standingOf) Took(string, string, int, int) {
	// The checker's test reads what a pass holds, never a take.
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

// Under Options.StandingBodies, with Options.StandingAfter unset, every pass on a package holds under Options.Standing
// the one answer the option gave for that package's ID, asked once for the package and with nothing: a dispatcher's
// pass, the pass of a rule that requires the dispatcher and the pass of an analyzer beside them all hold the same one.
// On a package the option answers nil for, no pass holds anything there, no nil under the key either. Without the
// option no pass holds anything under the key, and the graph holds the same actions either way.
func TestEveryPassOnAPackageHoldsItsOneStandingBodies(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	// The first package is the one nothing stands in.
	bare := roots[0].ID
	passes := newStandingPasses()
	dispatcher := passes.analyzer("dispatcher")
	analyzers := []*analysis.Analyzer{dispatcher, passes.analyzer("ruled", dispatcher), passes.analyzer("plain")}
	options := &Options{
		Tolerant: func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher },
		Standing: standingKey,
		StandingBodies: func(packageId string, after any) rule.StandingBodies {
			if passes.ask(packageId, after); packageId == bare {
				return nil
			}
			return &standingOf{packageId: packageId, after: after}
		},
	}
	actions := analyzeStanding(test, analyzers, roots, options)
	for _, root := range roots {
		dispatched := passes.passes["dispatcher "+root.PkgPath]
		for _, name := range []string{"dispatcher", "ruled", "plain"} {
			found := passes.passes[name+" "+root.PkgPath]
			if root.ID == bare {
				if found.keyed {
					test.Errorf("the pass of %s on %s, which the option answers nil for, holds %v under Standing, "+
						"want nothing there, no nil either", name, root.ID, found.standing)
				}
				continue
			}
			held, isHeld := found.standing.(*standingOf)
			if !isHeld || held.packageId != root.ID || held.after != nil || found.standing != dispatched.standing {
				test.Errorf("the pass of %s on %s holds %v under Standing, want the answer for its own package, "+
					"asked with nothing, and the very one the dispatcher's pass holds, %v", name, root.ID,
					found.standing, dispatched.standing)
			}
		}
		if asked := passes.asked[root.ID]; len(asked) != 1 {
			test.Errorf("the option was asked %d times for %s, want once for the package", len(asked), root.ID)
		}
	}

	unset := newStandingPasses()
	unsetDispatcher := unset.analyzer("dispatcher")
	unsetActions := analyzeStanding(test, []*analysis.Analyzer{unsetDispatcher, unset.analyzer("ruled",
		unsetDispatcher), unset.analyzer("plain")}, roots, &Options{Standing: standingKey})
	for key, found := range unset.passes {
		if found.keyed {
			test.Errorf("without the option the pass %s holds %v under Standing", key, found.standing)
		}
	}
	if actions != 3*len(roots) || unsetActions != actions {
		test.Errorf("the graph holds %d actions with the option and %d without, want one for each of three analyzers "+
			"on each of %d packages both ways", actions, unsetActions, len(roots))
	}
}

// Under Options.StandingAfter each dispatcher's action on a package follows that analyzer's action there, though the
// dispatcher does not require it: the followed analyzer has finished on the package before the dispatcher's pass
// runs, Options.StandingBodies is asked once for the package, with its result there, and the result is no input of the
// dispatcher's pass. The pass of a rule that requires the dispatcher holds the very answer the dispatcher's pass
// holds. The pass of an analyzer that follows neither may run before the answer, so it holds none. A dispatcher that
// requires the analyzer itself follows it by its own edge and holds its result as any input. One that requires it
// only for a listener no rule of the run has, an edge Options.ForDependents drops, follows it by the edge Analyze adds
// once that one is dropped, and holds no result of it.
func TestADispatcherFollowsTheStandingAnalyzerAndItsRuleHoldsTheSameAnswer(test *testing.T) {
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
			options := &Options{
				Tolerant: func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher },
				Standing: standingKey,
				StandingBodies: func(packageId string, after any) rule.StandingBodies {
					passes.ask(packageId, after)
					return &standingOf{packageId: packageId, after: after}
				},
				StandingAfter: followed,
			}
			if requirement.dropped {
				options.ForDependents = func(analyzer *analysis.Analyzer, required *analysis.Analyzer) bool {
					return analyzer == dispatcher && required == followed
				}
			}
			actions := analyzeStanding(test, []*analysis.Analyzer{dispatcher, passes.analyzer("ruled", dispatcher),
				passes.analyzer("plain")}, roots, options)
			for _, root := range roots {
				found := passes.passes["dispatcher "+root.PkgPath]
				held, isHeld := found.standing.(*standingOf)
				after := "followed on " + root.PkgPath
				if !found.followed || !isHeld || held.packageId != root.ID || held.after != after ||
					!slices.Equal(found.results, requirement.results) {
					test.Errorf("the dispatcher's pass on %s ran after the followed analyzer: %t, holds %v under "+
						"Standing and the results %v, want it after, holding the answer for its package asked with "+
						"%q, and the results %v", root.ID, found.followed, found.standing, found.results, after,
						requirement.results)
				}
				if ruled := passes.passes["ruled "+root.PkgPath]; !isHeld || ruled.standing != found.standing {
					test.Errorf("the pass of the rule that requires the dispatcher holds %v under Standing on %s, "+
						"want the very answer the dispatcher's pass holds, %v", ruled.standing, root.ID,
						found.standing)
				}
				if plain := passes.passes["plain "+root.PkgPath]; plain.keyed || len(plain.results) != 0 {
					test.Errorf("the pass of an analyzer that follows nothing holds %v under Standing and the "+
						"results %v on %s, want neither", plain.standing, plain.results, root.ID)
				}
				if asked := passes.asked[root.ID]; len(asked) != 1 {
					test.Errorf("the option was asked %d times for %s, want once for the package", len(asked),
						root.ID)
				}
			}
			if actions != 4*len(roots) {
				test.Errorf("the graph holds %d actions, want the dispatcher's, its rule's, the other analyzer's and "+
					"the followed one's on each of %d packages", actions, len(roots))
			}
		})
	}
}

// Where the analyzer Options.StandingAfter names failed on a package, the dispatcher there runs all the same, and
// neither its pass nor its rule's holds anything under Options.Standing, the option never asked: every doubt lints.
// The failure of the edge Analyze added is no part of what the dispatcher reads under Options.Failed, and it fails no
// dispatcher even where Options.Failed is unset, when an action runs past nothing else.
func TestAFailedStandingAnalyzerLeavesNothingStandingAndFailsNoDispatcher(test *testing.T) {
	test.Parallel()
	for _, failed := range []*analysis.Analyzer{failedKey, nil} {
		test.Run(fmt.Sprintf("Failed set: %t", failed != nil), func(test *testing.T) {
			test.Parallel()
			roots := timedModule(test)
			passes := newStandingPasses()
			dispatcher := passes.analyzer("dispatcher")
			analyzeStanding(test, []*analysis.Analyzer{dispatcher, passes.analyzer("ruled", dispatcher)}, roots,
				&Options{
					Failed:   failed,
					Tolerant: func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher },
					Standing: standingKey,
					StandingBodies: func(packageId string, after any) rule.StandingBodies {
						passes.ask(packageId, after)
						return &standingOf{packageId: packageId, after: after}
					},
					StandingAfter: passes.followedAnalyzer(errors.New("planted failure")),
				})
			for _, root := range roots {
				found, ran := passes.passes["dispatcher "+root.PkgPath]
				ruled, ruledRan := passes.passes["ruled "+root.PkgPath]
				asked := len(passes.asked[root.ID])
				if !ran || !found.followed || found.keyed || len(found.failed) != 0 || !ruledRan || ruled.keyed ||
					asked != 0 {
					test.Errorf("on %s, where the followed analyzer failed, the dispatcher ran: %t, after it: %t, "+
						"holding something under Standing: %t, and %v under Failed, its rule ran: %t, holding "+
						"something under Standing: %t, and the option was asked %d times, want both run, the "+
						"dispatcher after, nothing held under either key, and the option never asked", root.ID, ran,
						found.followed, found.keyed, found.failed, ruledRan, ruled.keyed, asked)
				}
			}
		})
	}
}

// A restored package runs nothing, so a dispatcher's action on one follows nothing and nothing is asked of it: with
// every package restored, the graph holds only the dispatcher's actions, the followed analyzer never runs, and the
// option is never asked, with Options.StandingAfter or without.
func TestADispatcherOnARestoredPackageFollowsNothing(test *testing.T) {
	test.Parallel()
	for _, follows := range []bool{true, false} {
		roots := timedModule(test)
		passes := newStandingPasses()
		dispatcher := passes.analyzer("dispatcher")
		options := &Options{
			Tolerant: func(analyzer *analysis.Analyzer) bool { return analyzer == dispatcher },
			Standing: standingKey,
			StandingBodies: func(packageId string, after any) rule.StandingBodies {
				passes.ask(packageId, after)
				return &standingOf{packageId: packageId, after: after}
			},
			Restored: func(*packages.Package) bool { return true },
			Restore:  func(*Action) error { return nil },
		}
		if follows {
			options.StandingAfter = passes.followedAnalyzer(nil)
		}
		actions := analyzeStanding(test, []*analysis.Analyzer{dispatcher}, roots, options)
		if actions != len(roots) || len(passes.finished) != 0 || len(passes.passes) != 0 || len(passes.asked) != 0 {
			test.Errorf("with every package restored and a followed analyzer: %t, the graph holds %d actions, the "+
				"followed analyzer ran on %v, %d passes ran and the option was asked of %v, want only the "+
				"dispatcher's action on each of %d packages, nothing run and nothing asked", follows, actions,
				passes.finished, len(passes.passes), passes.asked, len(roots))
		}
	}
}

// With Options.StandingBodies unset, Options.StandingAfter adds no action: the analyzer it names never runs, and the
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
