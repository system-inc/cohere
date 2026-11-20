package load

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// graphOf is packages by import path, each walking to walks[importPath] in that order.
func graphOf(walks map[string][]string) map[string]*inProcessPackage {
	byPath := map[string]*inProcessPackage{}
	for importPath, walked := range walks {
		byPath[importPath] = &inProcessPackage{listed: &listedPackage{ImportPath: importPath}, walked: walked}
	}
	return byPath
}

// walkedDeps is every package current reaches, sorted, by a walk from current alone: the lister's way before
// #zcc03r6, kept as the reference the merge is held to.
func walkedDeps(current *inProcessPackage, byPath map[string]*inProcessPackage) []string {
	seen := map[string]bool{}
	queue := slices.Clone(current.walked)
	for len(queue) > 0 {
		importPath := queue[0]
		queue = queue[1:]
		if seen[importPath] {
			continue
		}
		seen[importPath] = true
		queue = append(queue, byPath[importPath].walked...)
	}
	if len(seen) == 0 {
		return nil
	}
	return sortedKeys(seen)
}

// A package's Deps is everything it reaches, the packages its imports reach as well as its imports, sorted by import
// path whatever order it walks them in: root walks zeta before alpha, and alpha reaches beta only through mid.
func TestDepsHoldEveryPackageReachedInImportPathOrder(test *testing.T) {
	test.Parallel()
	byPath := graphOf(map[string][]string{
		"root":  {"zeta", "alpha"},
		"alpha": {"mid"},
		"mid":   {"beta"},
		"zeta":  {"beta", "gamma"},
		"beta":  nil,
		"gamma": nil,
	})
	var merging dependencyMerge
	for _, importPath := range []string{"beta", "gamma", "mid", "alpha", "zeta", "root"} {
		byPath[importPath].listed.Deps = merging.deps(byPath[importPath], byPath)
	}
	for importPath, want := range map[string][]string{
		"root":  {"alpha", "beta", "gamma", "mid", "zeta"},
		"alpha": {"beta", "mid"},
		"zeta":  {"beta", "gamma"},
		"beta":  nil,
	} {
		if got := byPath[importPath].listed.Deps; !slices.Equal(got, want) || (got == nil) != (want == nil) {
			test.Errorf("%s's Deps are %q, want %q", importPath, got, want)
		}
	}
	// Each Deps is its own: merging reuses its buffers, so a Deps that shared one would change under the next.
	if !slices.Equal(byPath["alpha"].listed.Deps, []string{"beta", "mid"}) {
		test.Errorf("alpha's Deps changed to %q as later packages were merged", byPath["alpha"].listed.Deps)
	}
}

// Over random acyclic graphs, merged in an order that puts each package after what it walks to, every package's Deps
// is the walk's, element for element.
func TestDepsMergedAreTheWalksOnRandomGraphs(test *testing.T) {
	test.Parallel()
	for seed := range uint64(50) {
		random := rand.New(rand.NewPCG(seed, seed))
		count := 2 + random.IntN(60)
		// Names in a random order, so a package's place in the order says nothing of where its name sorts.
		names := make([]string, count)
		for index, permuted := range random.Perm(count) {
			names[index] = fmt.Sprintf("example.com/package%02d", permuted)
		}
		walks := map[string][]string{}
		for index, name := range names {
			walks[name] = nil
			// A package walks only to one placed before it, so the graph has no cycle and names is a valid order.
			for _, below := range random.Perm(index) {
				if random.IntN(4) == 0 {
					walks[name] = append(walks[name], names[below])
				}
			}
		}
		byPath := graphOf(walks)
		var merging dependencyMerge
		for _, name := range names {
			byPath[name].listed.Deps = merging.deps(byPath[name], byPath)
		}
		for _, name := range names {
			want := walkedDeps(byPath[name], byPath)
			if got := byPath[name].listed.Deps; !slices.Equal(got, want) || (got == nil) != (want == nil) {
				test.Fatalf("seed %d, %s: merged %q, walked %q", seed, name, got, want)
			}
		}
	}
}

// A package walked to that is already in the Deps of the package with the most is passed over, and the answer is still
// everything reached: root walks to wide, the most Deps, to mid and leaf, both inside wide's, to mid twice, and to
// side, outside wide's Deps and reaching what wide does not, so a merge that passed over wide itself, or side, or that
// missed what mid reaches, shows.
func TestDepsPassOverAWalkInsideTheMostDepsAndStillHoldEverything(test *testing.T) {
	test.Parallel()
	byPath := graphOf(map[string][]string{
		"root":  {"mid", "wide", "leaf", "side", "mid"},
		"wide":  {"mid", "other"},
		"mid":   {"leaf", "deep"},
		"side":  {"far"},
		"other": nil,
		"leaf":  nil,
		"deep":  nil,
		"far":   nil,
	})
	var merging dependencyMerge
	for _, importPath := range []string{"leaf", "deep", "far", "other", "mid", "wide", "side", "root"} {
		byPath[importPath].listed.Deps = merging.deps(byPath[importPath], byPath)
	}
	want := []string{"deep", "far", "leaf", "mid", "other", "side", "wide"}
	if got := byPath["root"].listed.Deps; !slices.Equal(got, want) {
		test.Errorf("root's Deps are %q, want %q", got, want)
	}
	if walked := walkedDeps(byPath["root"], byPath); !slices.Equal(walked, want) {
		test.Fatalf("the fixture's walk reaches %q, want %q", walked, want)
	}
}
