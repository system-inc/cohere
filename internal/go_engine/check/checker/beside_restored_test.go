package checker

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// Options.Beside is rooted on each package given that is not restored, and on none that is, since nothing on a
// restored package runs (#5vbs1np, must 4): over packages half of which are restored, Graph.Beside holds one action on
// each of the others, and none on a restored one.
func TestBesideIsRootedOnlyOnPackagesNotRestored(test *testing.T) {
	test.Parallel()
	roots := timedModule(test)
	identifiers := make([]string, 0, len(roots))
	for _, root := range roots {
		identifiers = append(identifiers, root.ID)
	}
	slices.Sort(identifiers)
	restoredPackages := map[string]bool{identifiers[1]: true, identifiers[3]: true}
	noop := func(name string) *analysis.Analyzer {
		return &analysis.Analyzer{Name: name, Doc: name + " does nothing", Run: func(*analysis.Pass) (any, error) {
			return nil, nil
		}}
	}
	graph, analyzeError := Analyze([]*analysis.Analyzer{noop("rooted")}, roots, &Options{
		Restored: func(restoring *packages.Package) bool { return restoredPackages[restoring.ID] },
		Restore:  func(*Action) error { return nil },
		Beside:   noop("beside"),
	})
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	var besides []string
	for _, action := range graph.Beside {
		besides = append(besides, action.Package.ID)
	}
	slices.Sort(besides)
	want := slices.DeleteFunc(slices.Clone(identifiers), func(identifier string) bool {
		return restoredPackages[identifier]
	})
	if !slices.Equal(besides, want) {
		test.Errorf("Beside is rooted on %v, want the packages not restored, %v", besides, want)
	}
}
