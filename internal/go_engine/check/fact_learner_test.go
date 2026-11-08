package check

import (
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// An analyzer learns facts when it declares them or when anything it requires does, at any depth, as a rule learns
// them through the dispatcher it requires; one whose whole requirement graph declares none learns none.
func TestFactLearnerFindsFactsThroughRequirements(test *testing.T) {
	test.Parallel()

	dispatcher := &analysis.Analyzer{
		Name: "dispatcher", Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	}
	learning := &analysis.Analyzer{Name: "learning", Requires: []*analysis.Analyzer{dispatcher}}
	if learner := FactLearner(learning, []*analysis.Analyzer{learning}); learner != ctrlflow.Analyzer {
		test.Fatalf("want ctrlflow, two requirements down, as the learner, got %v", learner)
	}
	ownLearner := FactLearner(ctrlflow.Analyzer, []*analysis.Analyzer{ctrlflow.Analyzer})
	if ownLearner != ctrlflow.Analyzer {
		test.Fatalf("want ctrlflow its own learner, got %v", ownLearner)
	}
	quiet := &analysis.Analyzer{Name: "quiet", Requires: []*analysis.Analyzer{inspect.Analyzer, inspect.Analyzer}}
	if learner := FactLearner(quiet, []*analysis.Analyzer{quiet}); learner != nil {
		test.Fatalf("want no learner under inspect alone, got %s", learner.Name)
	}
}

// A rule learns facts through its dispatcher only when its run holds a rule whose listener requires an analyzer that
// declares them. A listener registered on the same dispatcher for a rule the run leaves out teaches the run nothing,
// as the rename's run of the naming rules alone must find, since the checker never runs what only that listener
// requires (#r6d67h3).
func TestARunLearnsFactsThroughADispatcherOnlyForItsOwnRules(test *testing.T) {
	test.Parallel()

	learning := &analysis.Analyzer{
		Name: "planted_learning", Doc: "planted: declares a fact and finds nothing",
		FactTypes: []analysis.Fact{new(plantedFact)},
		Run:       func(*analysis.Pass) (any, error) { return nil, nil },
	}
	dispatcher := dispatch.NewSyntax()
	quietListener := &rule.Listener{
		Nodes: []ast.Node{(*ast.Ident)(nil)},
		Node:  func(*rule.Context, ast.Node, []ast.Node) {},
	}
	learningListener := &rule.Listener{
		Requires: []*analysis.Analyzer{learning},
		Nodes:    []ast.Node{(*ast.Ident)(nil)},
		Node:     func(*rule.Context, ast.Node, []ast.Node) {},
	}
	quiet := dispatcher.RuleAnalyzer("planted/quiet", "planted_quiet", "planted: finds nothing", quietListener)
	learner := dispatcher.RuleAnalyzer("planted/learner", "planted_learner", "planted: finds nothing",
		learningListener)
	if found := FactLearner(quiet, []*analysis.Analyzer{quiet}); found != nil {
		test.Errorf("want no learner for a run of the quiet rule alone, got %s, learned through a listener the run "+
			"leaves out", found.Name)
	}
	if found := FactLearner(quiet, []*analysis.Analyzer{quiet, learner}); found != learning {
		test.Errorf("want the planted learner, through the dispatcher, once the run holds the rule requiring it, "+
			"got %v", found)
	}
	if found := FactLearner(learner, []*analysis.Analyzer{learner}); found != learning {
		test.Errorf("want the planted learner for its own rule, got %v", found)
	}
}
