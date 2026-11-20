package check

import (
	"maps"
	"slices"
	"testing"
)

// expectUntouched fails when the walk over candidates leaves other than want untouched, named by label.
func expectUntouched(
	test *testing.T,
	label string,
	candidates []walkCandidate,
	changes walkChanges,
	listed map[string]bool,
	want ...string,
) {
	test.Helper()
	if got := untouchedPaths(untouchedOf(candidates, changes, listed)); !slices.Equal(got, want) {
		test.Errorf("%s: the walk left %v untouched, want %v", label, got, want)
	}
}

// A closure's membership is read through every import below a reader, and each importer's answer is kept as it truly
// is: r changed its imports, a imports r, and m1 and m2 both import a and read their closure's membership, so both are
// visited, m2 asked after m1 has met a. Ask only a reader's direct imports and neither is; keep a's answer false once
// it is found to hold r and m2 is left untouched.
func TestEveryMembershipReaderAboveAReshapedPackageIsVisited(test *testing.T) {
	test.Parallel()

	graph := newWalkGraph(map[string][]string{"r": nil, "a": {"r"}, "m1": {"a"}, "m2": {"a"}}, "r")
	candidates := []walkCandidate{
		readingMembership(graph.candidate("m1", false)),
		readingMembership(graph.candidate("m2", false)),
	}
	changes := walkChanges{changed: graph.changed, reshaped: graph.changed}
	expectUntouched(test, "membership", candidates, changes, graph.listed())
}

// A package's sets unite every unfound path and every own fact path of each, beside their paths, so a package with a
// set of each meets what changed by either. Drop either kind from the union and its paths are never read.
func TestDependenceSetsUniteTheirUnfoundAndOwnFactPaths(test *testing.T) {
	test.Parallel()

	united := unitedDependence(
		DependenceSet{Paths: map[string]bool{"read": true}},
		DependenceSet{UnfoundPaths: map[string]bool{"unfound": true}},
		DependenceSet{OwnFactPaths: map[string]bool{"deprecated": true}},
	)
	for _, each := range []struct {
		kind  string
		paths map[string]bool
		want  string
	}{
		{"paths", united.Paths, "read"},
		{"unfound paths", united.UnfoundPaths, "unfound"},
		{"own fact paths", united.OwnFactPaths, "deprecated"},
	} {
		if got := slices.Sorted(maps.Keys(each.paths)); !slices.Equal(got, []string{each.want}) {
			test.Errorf("the united %s are %v, want only %s", each.kind, got, each.want)
		}
	}
}

// What a candidate's set names is met by path, and test variants share their package's path: a variant of leaf, its
// ID not its path, is visited for reading c, which changed, and so a reader of leaf is visited, and a reader whose set
// is its whole closure, importing a changed variant of leaf, is visited too. Mark the visited variant affected by its
// ID, or find a changed import by its ID, and the reader is left untouched.
func TestTheWalkMeetsATestVariantByItsPackagesPath(test *testing.T) {
	test.Parallel()

	visitedVariant := newWalkGraph(map[string][]string{"c": nil, "leaf": {"c"}, "reader": {"leaf"}}, "c")
	visitedVariant.byPath["leaf"].ID = "leaf [leaf.test]"
	expectUntouched(test, "a visited variant", []walkCandidate{
		visitedVariant.candidate("leaf", false, "c"),
		visitedVariant.candidate("reader", false, "leaf"),
	}, walkChanges{changed: visitedVariant.changed}, visitedVariant.listed())

	changedVariant := newWalkGraph(map[string][]string{"leaf": nil, "reader": {"leaf"}}, "leaf")
	changedVariant.byPath["leaf"].ID = "leaf [leaf.test]"
	expectUntouched(test, "a changed variant", []walkCandidate{changedVariant.candidate("reader", true)},
		walkChanges{changed: changedVariant.changed}, changedVariant.listed())
}

// An unfound path meets what changed when its package is in the reader's closure and affected, visited in the walk
// though outside D: bridge reads a, which changed, so bridge is visited, and reader, whose Closure lookup found nothing
// at bridge, which it imports, is visited too. Ask only whether the unfound path is in D and reader is left untouched.
func TestAnUnfoundPathMeetsAPackageTheWalkVisited(test *testing.T) {
	test.Parallel()

	graph := newWalkGraph(map[string][]string{"a": nil, "bridge": {"a"}, "reader": {"bridge"}}, "a")
	expectUntouched(test, "unfound at a visited package", []walkCandidate{
		graph.candidate("bridge", false, "a"),
		unfound(graph.candidate("reader", false), "bridge"),
	}, walkChanges{changed: graph.changed}, graph.listed())
}

// A package the build does not list has nothing to render as the host's did, so a set that names one meets what
// changed, as a path it read or as a package whose deprecations it read, and a whole closure that holds a package the
// walk visited meets it too, though nothing changed: bridge read gone, which the build does not list, so it is
// visited; deprecations read gone's deprecations; whole imports bridge. Drop the unlisted own fact path, or read a
// whole closure against D alone, and deprecations or whole is left untouched.
func TestAnUnlistedPathMeetsWhatChangedThroughEachRead(test *testing.T) {
	test.Parallel()

	graph := newWalkGraph(map[string][]string{"bridge": nil, "deprecations": nil, "whole": {"bridge"}})
	expectUntouched(test, "unlisted", []walkCandidate{
		graph.candidate("bridge", false, "gone"),
		ownFacts(graph.candidate("deprecations", false), "gone"),
		graph.candidate("whole", true),
	}, walkChanges{}, graph.listed())
}
