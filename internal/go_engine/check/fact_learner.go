package check

import "golang.org/x/tools/go/analysis"

// FactLearner is the first analyzer that learns facts among analyzer and everything it requires, directly or not,
// depth first, or nil when none does. Run analyzes every dependency of the packages it is given with each such
// analyzer, to learn its facts, and those dependencies come back without their syntax and type information (see
// checker.Analyze). A dispatcher requires what every listener registered on it requires, so a rule with no facts of
// its own learns them through its dispatcher as soon as one listener there requires an analyzer that does.
func FactLearner(analyzer *analysis.Analyzer) *analysis.Analyzer {
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
		for _, required := range current.Requires {
			if learner := find(required); learner != nil {
				return learner
			}
		}
		return nil
	}
	return find(analyzer)
}
