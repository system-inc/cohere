package checker

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// An action on a restored package depends on nothing it requires, since nothing on a restored package runs, and an
// action on any other package depends on each analyzer it requires there: over packages half of which are restored,
// a root requiring one analyzer has that one dependency on each package not restored and none on a restored one.
func TestARestoredPackagesActionRequiresNothing(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	identifiers := make([]string, 0, len(roots))
	for _, root := range roots {
		identifiers = append(identifiers, root.ID)
	}
	slices.Sort(identifiers)
	restoredPackages := map[string]bool{identifiers[1]: true, identifiers[3]: true}
	required := &analysis.Analyzer{Name: "required", Doc: "required does nothing",
		Run: func(*analysis.Pass) (any, error) { return nil, nil }}
	requiring := &analysis.Analyzer{Name: "requiring", Doc: "requiring does nothing but require",
		Requires: []*analysis.Analyzer{required}, Run: func(*analysis.Pass) (any, error) { return nil, nil }}
	graph, analyzeError := Analyze([]*analysis.Analyzer{requiring}, roots, &Options{
		Restored: func(restoring *packages.Package) bool { return restoredPackages[restoring.ID] },
		Restore:  func(*Action) error { return nil },
	})
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		want := 1
		if restoredPackages[action.Package.ID] {
			want = 0
		}
		if len(action.Deps) != want {
			test.Errorf("the root on %s, restored %t, depends on %d actions, want %d", action.Package.ID,
				restoredPackages[action.Package.ID], len(action.Deps), want)
		}
	}
}
