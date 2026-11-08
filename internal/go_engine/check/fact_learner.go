package check

import (
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// FactLearner is the first analyzer that learns facts among analyzer and everything it requires in a run of run's
// analyzers, directly or not, depth first, or nil when none does. Run analyzes every dependency of the packages it is
// given with each such analyzer, to learn its facts, and those dependencies come back without their syntax and type
// information (see checker.Analyze). A dispatcher requires what its listeners do, but in a run only what a rule of
// that run requires too (requiredInRun), so a rule learns facts through its dispatcher exactly when the run holds a
// rule whose listener requires an analyzer that does, never because one is merely registered.
func FactLearner(analyzer *analysis.Analyzer, run []*analysis.Analyzer) *analysis.Analyzer {
	requires := requiredInRun(run)
	seen := map[*analysis.Analyzer]bool{}
	var find func(current *analysis.Analyzer) *analysis.Analyzer
	find = func(current *analysis.Analyzer) *analysis.Analyzer {
		if seen[current] {
			return nil
		}
		seen[current] = true
		if len(current.FactTypes) > 0 {
			return current
		}
		for _, required := range requires(current) {
			if learner := find(required); learner != nil {
				return learner
			}
		}
		return nil
	}
	return find(analyzer)
}

// requiredInRun is what each analyzer requires as a run of run's analyzers builds its graph: every requirement, but a
// dispatcher's for its listeners alone (dispatch.ListenerRequirement) only where an analyzer of the run, or below
// it, that requires the dispatcher requires it too. It is the copied checker's decision under
// checker.Options.ForDependents, made over analyzers rather than actions, which on every package is the same: each
// package's actions require what their analyzers do. FactLearner and newCacheAnalyzers both read it, so what the
// rename and the cache believe a run learns is what the checker runs.
func requiredInRun(run []*analysis.Analyzer) func(analyzer *analysis.Analyzer) []*analysis.Analyzer {
	requirers := requirersBelow(run)
	return func(analyzer *analysis.Analyzer) []*analysis.Analyzer {
		kept := make([]*analysis.Analyzer, 0, len(analyzer.Requires))
		for _, required := range analyzer.Requires {
			if dispatch.ListenerRequirement(analyzer, required) && !slices.ContainsFunc(requirers[analyzer],
				func(requirer *analysis.Analyzer) bool { return slices.Contains(requirer.Requires, required) }) {
				continue
			}
			kept = append(kept, required)
		}
		return kept
	}
}
