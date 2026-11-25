package check

import (
	"math/rand/v2"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"
)

// walkGraph is a listing of packages by path, each importing those named, for the walk's tests: changed is D, by path.
type walkGraph struct {
	byPath  map[string]*packages.Package
	changed map[string]bool
}

// newWalkGraph is the packages of imports, each by path with the paths it imports, every import listed too.
func newWalkGraph(imports map[string][]string, changed ...string) walkGraph {
	graph := walkGraph{byPath: map[string]*packages.Package{}, changed: map[string]bool{}}
	for path := range imports {
		graph.byPath[path] = &packages.Package{ID: path, PkgPath: path, Imports: map[string]*packages.Package{}}
	}
	for path, imported := range imports {
		for _, each := range imported {
			graph.byPath[path].Imports[each] = graph.byPath[each]
		}
	}
	for _, path := range changed {
		graph.changed[path] = true
	}
	return graph
}

// listed is every path of the graph.
func (graph walkGraph) listed() map[string]bool {
	listed := map[string]bool{}
	for path := range graph.byPath {
		listed[path] = true
	}
	return listed
}

// candidate is the package at path with set, its dependence set's paths, or the whole closure when whole.
func (graph walkGraph) candidate(path string, whole bool, set ...string) walkCandidate {
	paths := map[string]bool{}
	for _, each := range set {
		paths[each] = true
	}
	return walkCandidate{candidate: graph.byPath[path], dependence: DependenceSet{WholeClosure: whole, Paths: paths}}
}

// unfound is candidate with the paths of Closure lookups that found nothing (DependenceSet.UnfoundPaths).
func unfound(candidate walkCandidate, paths ...string) walkCandidate {
	candidate.dependence.UnfoundPaths = map[string]bool{}
	for _, path := range paths {
		candidate.dependence.UnfoundPaths[path] = true
	}
	return candidate
}

// ownFacts is candidate with the packages whose deprecations it read (DependenceSet.OwnFactPaths).
func ownFacts(candidate walkCandidate, paths ...string) walkCandidate {
	candidate.dependence.OwnFactPaths = map[string]bool{}
	for _, path := range paths {
		candidate.dependence.OwnFactPaths[path] = true
	}
	return candidate
}

// readingMembership is candidate reading its import closure's membership too (DependenceSet.ClosureMembership).
func readingMembership(candidate walkCandidate) walkCandidate {
	candidate.dependence.ClosureMembership = true
	return candidate
}

// untouchedPaths are the paths of untouched, sorted.
func untouchedPaths(untouched map[*packages.Package]bool) []string {
	paths := make([]string, 0, len(untouched))
	for each := range untouched {
		paths = append(paths, each.PkgPath)
	}
	slices.Sort(paths)
	return paths
}

// The walk's fixed point (untouchedOf), with a changed, and r changed with another import: b reads a, so b is visited;
// c reads only b, which is visited, so c is too, though nothing it reads is in D; d reads nothing; e, whose set is the
// whole closure, imports a; f's whole closure is g, which nothing changed; h read a package the build does not list; m
// reads the membership of a closure holding r, and n of one holding only a, which changed with the host's imports and
// name, so only m is visited by it; u's Closure lookup found nothing at a, which changed, outside its closure, which is
// no read, and v's nothing at a in its closure; w read the deprecations of b, visited but its files the host's, and x
// those of a, whose files changed. The candidates are taken c first, so a walk of one pass in their order leaves c
// untouched. Only d, f, g, n, u and w are left untouched.
func TestTheWalkVisitsWhatReadsWhatChangedToItsFixedPoint(test *testing.T) {
	test.Parallel()

	graph := newWalkGraph(map[string][]string{
		"a": nil, "b": {"a"}, "c": {"b"}, "d": nil, "e": {"a"}, "f": {"g"}, "g": nil, "h": nil, "r": nil,
		"m": {"r"}, "n": {"a"}, "u": nil, "v": {"a"}, "w": {"b"}, "x": {"a"},
	}, "a", "r")
	candidates := []walkCandidate{
		graph.candidate("c", false, "b"),
		graph.candidate("b", false, "a"),
		graph.candidate("d", false),
		graph.candidate("e", true),
		graph.candidate("f", true),
		graph.candidate("g", false),
		graph.candidate("h", false, "gone"),
		readingMembership(graph.candidate("m", false)),
		readingMembership(graph.candidate("n", false)),
		unfound(graph.candidate("u", false), "a"),
		unfound(graph.candidate("v", false), "a"),
		ownFacts(graph.candidate("w", false), "b"),
		ownFacts(graph.candidate("x", false), "a"),
	}
	want := []string{"d", "f", "g", "n", "u", "w"}
	changes := walkChanges{changed: graph.changed, rewritten: graph.changed, reshaped: map[string]bool{"r": true}}
	if got := untouchedPaths(untouchedOf(candidates, changes, graph.listed())); !slices.Equal(got, want) {
		test.Errorf("the walk left %v untouched, want %v", got, want)
	}
}

// The fixed point reads only D and the sets, so it is the same in any order the candidates are taken (Square's edge 3
// on #154dr43): a chain of ten, each reading the one before, the first reading D, shuffled a hundred ways, visits every
// link of the chain and leaves the one reading nothing untouched.
func TestTheWalkIsTheSameInAnyOrder(test *testing.T) {
	test.Parallel()

	imports := map[string][]string{"root": nil, "alone": nil}
	candidates := []walkCandidate{}
	previous := "root"
	links := []string{"l0", "l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9"}
	for _, link := range links {
		imports[link] = []string{previous}
		previous = link
	}
	graph := newWalkGraph(imports, "root")
	previous = "root"
	for _, link := range links {
		candidates = append(candidates, graph.candidate(link, false, previous))
		previous = link
	}
	candidates = append(candidates, graph.candidate("alone", false))
	shuffler := rand.New(rand.NewPCG(1, 2))
	for range 100 {
		shuffler.Shuffle(len(candidates), func(left, right int) {
			candidates[left], candidates[right] = candidates[right], candidates[left]
		})
		if got := untouchedPaths(untouchedOf(candidates, walkChanges{changed: graph.changed},
			graph.listed())); !slices.Equal(got,
			[]string{"alone"}) {
			test.Fatalf(
				"taken in the order %v, the walk left %v untouched, want only alone",
				candidatePaths(candidates),
				got,
			)
		}
	}
}

// candidatePaths are the paths of candidates, in order.
func candidatePaths(candidates []walkCandidate) []string {
	paths := make([]string, 0, len(candidates))
	for _, each := range candidates {
		paths = append(paths, each.candidate.PkgPath)
	}
	return paths
}

// A package's sets unite into the whole closure when any is the whole of it, and into every path of each otherwise,
// with the closure's membership when any reads it.
func TestDependenceSetsUnite(test *testing.T) {
	test.Parallel()

	paths := func(names ...string) DependenceSet {
		set := DependenceSet{Paths: map[string]bool{}}
		for _, name := range names {
			set.Paths[name] = true
		}
		return set
	}
	if united := unitedDependence(paths("a"), paths("b", "c")); dependenceNames(united)[0] != "a" ||
		len(united.Paths) != 3 || united.WholeClosure {
		test.Errorf("a and b, c unite to %v, want a, b and c", dependenceNames(united))
	}
	if united := unitedDependence(paths("a"), DependenceSet{WholeClosure: true}); !united.WholeClosure {
		test.Errorf("a and the whole closure unite to %v, want the whole closure", dependenceNames(united))
	}
	if united := unitedDependence(paths("a"), DependenceSet{ClosureMembership: true}); !united.ClosureMembership ||
		len(united.Paths) != 1 {
		test.Errorf("a and the closure's membership unite to %v, want both", dependenceNames(united))
	}
}
