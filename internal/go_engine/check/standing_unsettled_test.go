package check

import (
	"go/ast"
	"reflect"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// everyBodyStanding is a package's standing bodies under which every function stands, every rule is confined and the
// host holds no finding on any file, counting each question of the host's findings and each take a rule is told of.
type everyBodyStanding struct {
	asked *atomic.Int32
	took  *atomic.Int32
}

// Stands answers that every function stands.
func (everyBodyStanding) Stands(string, int) bool { return true }

// Confines answers that every rule is confined.
func (everyBodyStanding) Confines(string) bool { return true }

// Findings counts the question and answers that the host holds no finding on the file, a holding all the same, so a
// rule is confined there.
func (standing everyBodyStanding) Findings(string, string) ([]rule.InheritedFinding, bool) {
	standing.asked.Add(1)
	return []rule.InheritedFinding{}, true
}

// Took counts the take.
func (standing everyBodyStanding) Took(string, string, int, int) {
	standing.took.Add(1)
}

// A run none of whose rules requires ctrlflow roots the analyzer the facts' reach settles by nowhere (settlingIn), so
// with Options.StandingAfter that same analyzer, the standing bodies are handed to no pass (#e4rd5dw): following it
// there would run it, and the answers it learns would reach the entries. With a StandingBodies that would leave every
// body of every package standing and confine every rule, it is never asked, no confined rule takes anything of the
// host's, the settling analyzer runs on no package, and the cold run's findings and entries are a cold run's without
// the standing pair.
func TestARunThatSettlesNothingHandsNoStandingBodies(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	dispatcher := dispatch.NewSyntax()
	listener := &rule.Listener{
		Nodes: []ast.Node{(*ast.FuncDecl)(nil)},
		Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
			if declaration, isDeclaration := node.(*ast.FuncDecl); isDeclaration {
				context.Report(analysis.Diagnostic{Pos: declaration.Name.Pos(), Message: "declares " +
					declaration.Name.Name})
			}
		},
	}
	dispatchedName := rule.Namespace + "test-standing-unsettled"
	rules := []rule.Rule{
		{
			Name:   dispatchedName,
			Origin: rule.OriginHouse,
			Analyzer: dispatcher.RuleAnalyzer(dispatchedName, "standing_unsettled",
				"reports every function declaration", listener),
			Listener: listener,
		},
		houseRule(targetAnalyzer),
		packageClauseRule,
	}
	for _, enabled := range rules {
		if requiresAnalyzer(enabled.Analyzer, ctrlflow.Analyzer) {
			test.Fatalf("the rule %s requires ctrlflow, so the run would settle", enabled.Name)
		}
	}

	var plainRuns atomic.Int32
	var standingRuns atomic.Int32
	var bodiesAsked atomic.Int32
	standing := everyBodyStanding{asked: &atomic.Int32{}, took: &atomic.Int32{}}
	plainDirectory, standingDirectory := test.TempDir(), test.TempDir()
	plainCaching, standingCaching := cachingAt(plainDirectory, rules), cachingAt(standingDirectory, rules)
	run := func(analyzed []*packages.Package, caching *Caching, options Options) *Result {
		test.Helper()
		options.Cache = NewCacheSession(caching, rules)
		result, runError := Run(analyzed, rules, options)
		if runError != nil {
			test.Fatalf("running %d rules: %v", len(rules), runError)
		}
		return result
	}
	plain := run(ownLoad(test, "."), plainCaching,
		Options{FactReachSettle: settlingAnalyzer("settling_plain", &plainRuns, false)})
	settling := settlingAnalyzer("settling_standing", &standingRuns, false)
	standingResult := run(ownLoad(test, "."), standingCaching, Options{FactReachSettle: settling,
		StandingAfter: settling, StandingBodies: func(string, any) rule.StandingBodies {
			bodiesAsked.Add(1)
			return standing
		}})

	if bodiesAsked.Load() != 0 || standing.asked.Load() != 0 || standing.took.Load() != 0 {
		test.Errorf("the standing bodies were asked %d times, the host's findings %d times, and %d takes were told, "+
			"want none of any", bodiesAsked.Load(), standing.asked.Load(), standing.took.Load())
	}
	if standingRuns.Load() != 0 || plainRuns.Load() != 0 || standingResult.FactReachUnsettled != withoutCtrlflow {
		test.Errorf("the settling analyzer ran %d times with the standing pair and %d without, unsettled %q, want "+
			"no run, for %q", standingRuns.Load(), plainRuns.Load(), standingResult.FactReachUnsettled, withoutCtrlflow)
	}
	if len(plain.Findings) == 0 || !reflect.DeepEqual(standingResult.Cache, plain.Cache) ||
		!reflect.DeepEqual(withoutSettled(standingResult), withoutSettled(plain)) {
		test.Errorf("with the standing pair the cold run is\n%+v\nwant the run without it\n%+v",
			withoutSettled(standingResult), withoutSettled(plain))
	}
	requireSameStores(test, plainDirectory, plainCaching, standingDirectory, standingCaching)
}
