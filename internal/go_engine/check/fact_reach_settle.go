package check

import (
	"cmp"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
)

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
