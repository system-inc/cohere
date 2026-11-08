package check

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
)

// An analyzer learns facts when it declares them or when anything it requires does, at any depth, as a rule learns
// them through the dispatcher it requires; one whose whole requirement graph declares none learns none.
func TestFactLearnerFindsFactsThroughRequirements(test *testing.T) {
	test.Parallel()

	dispatcher := &analysis.Analyzer{
		Name: "dispatcher", Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	}
	learning := &analysis.Analyzer{Name: "learning", Requires: []*analysis.Analyzer{dispatcher}}
	if learner := FactLearner(learning); learner != ctrlflow.Analyzer {
		test.Fatalf("want ctrlflow, two requirements down, as the learner, got %v", learner)
	}
	if learner := FactLearner(ctrlflow.Analyzer); learner != ctrlflow.Analyzer {
		test.Fatalf("want ctrlflow its own learner, got %v", learner)
	}
	quiet := &analysis.Analyzer{Name: "quiet", Requires: []*analysis.Analyzer{inspect.Analyzer, inspect.Analyzer}}
	if learner := FactLearner(quiet); learner != nil {
		test.Fatalf("want no learner under inspect alone, got %s", learner.Name)
	}
}
