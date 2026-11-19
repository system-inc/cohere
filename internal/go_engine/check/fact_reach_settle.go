package check

import (
	"cmp"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
)

// withoutCtrlflow is why a run roots no Options.FactReachSettle: none of its rules requires ctrlflow
// (Result.FactReachUnsettled).
const withoutCtrlflow = "ctrlflow not in the run"

// settlingIn is the analyzer the facts' reach settles by in a run of analyzers, settle, and why there is none when it
// is nil though settle is not. The settling analyzer requires ctrlflow, whose run on a package has the resolver learn
// it, and the save then writes the package's answers, each kind's computed whole, into its entry; a run whose rules
// run ctrlflow learns every package so already, but in one whose rules do not, settling would write answers a run
// without it never writes. So only beside rules that require ctrlflow does a run settle, and the facts' reach adds no
// analyzer that reaches an entry or a key.
func settlingIn(analyzers []*analysis.Analyzer, settle *analysis.Analyzer) (*analysis.Analyzer, string) {
	if settle == nil {
		return nil, ""
	}
	if !slices.ContainsFunc(analyzers, func(analyzer *analysis.Analyzer) bool {
		return requiresAnalyzer(analyzer, ctrlflow.Analyzer)
	}) {
		return nil, withoutCtrlflow
	}
	return settle, ""
}

// requiresAnalyzer is whether analyzer is wanted or requires it, directly or through what it requires.
func requiresAnalyzer(analyzer *analysis.Analyzer, wanted *analysis.Analyzer) bool {
	if analyzer == wanted {
		return true
	}
	return slices.ContainsFunc(analyzer.Requires, func(required *analysis.Analyzer) bool {
		return requiresAnalyzer(required, wanted)
	})
}

// settledOf is Options.FactReachSettle's result on each package graphs rooted it on, by package ID, and the packages
// it did not finish on, sorted. A later graph's action on a package is the one that counts, as in rootActions: the
// second wave analyzes again a package of the first that it could not restore.
func settledOf(graphs []*checker.Graph) (map[string]any, []Crash) {
	settled := map[string]any{}
	failed := map[string]Crash{}
	for _, graph := range graphs {
		for _, action := range graph.Beside {
			if action.Err != nil {
				delete(settled, action.Package.ID)
				failed[action.Package.ID] = Crash{File: firstFile(action.Package), Error: action.Err.Error()}
				continue
			}
			delete(failed, action.Package.ID)
			settled[action.Package.ID] = action.Result
		}
	}
	var crashes []Crash
	for _, crash := range failed {
		crashes = append(crashes, crash)
	}
	slices.SortFunc(crashes, func(left Crash, right Crash) int {
		return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Error, right.Error))
	})
	return settled, crashes
}
