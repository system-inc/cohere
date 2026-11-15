package check

import (
	"fmt"
	"go/types"
	"math/rand/v2"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// TestATestVariantsClosureResolvesTheVariant pins the closure index's one trap (#nrk4zt0): "p [p.test]" shares its
// path with "p", so a test variant that imports it must find it, and a plain importer the plain p, at the same path. An
// index by path alone across the build answers both with whichever p it met first. r is the first candidate, so the
// plain p is numbered first.
func TestATestVariantsClosureResolvesTheVariant(test *testing.T) {
	test.Parallel()

	plain := closureFixturePackage("example.com/p", "example.com/p", true)
	variant := closureFixturePackage("example.com/p [example.com/p.test]", "example.com/p", true)
	plainImporter := closureFixturePackage("example.com/r", "example.com/r", true, plain)
	variantImporter := closureFixturePackage("example.com/q [example.com/p.test]", "example.com/q", true, variant)
	external := closureFixturePackage("example.com/p_test [example.com/p.test]", "example.com/p_test", true, variant,
		variantImporter)
	index := newClosureIndex([]*packages.Package{plainImporter, variantImporter, external})

	for _, each := range []struct {
		candidate *packages.Package
		want      *packages.Package
	}{
		{candidate: plainImporter, want: plain},
		{candidate: variantImporter, want: variant},
		{candidate: external, want: variant},
	} {
		if index.walks(each.candidate) {
			test.Fatalf("%s is walked, want it read from the index: its closure holds one p", each.candidate.ID)
		}
		if declared := index.declaring(each.candidate, "example.com/p"); declared != each.want {
			test.Errorf("%s declares example.com/p as %s, want %s", each.candidate.ID, closureFixtureName(declared),
				each.want.ID)
		}
		if typed := index.typesOf(each.candidate, "example.com/p"); typed != each.want.Types {
			test.Errorf("%s has the types of example.com/p from another package than %s", each.candidate.ID,
				each.want.ID)
		}
	}
}

// TestTheClosureIndexAnswersAsTheWalks holds the closure index to the walks it replaced, kept here as they were, over a
// build of plain packages, the test variants of several of them as go test builds them, packages of no types that hide
// what only they reach, and candidates whose closures hold two packages of one path, which are walked as before. Every
// candidate the index answers is asked every path of the build and one it lacks, from every core at once, so `-race`
// sees the workers share it. Equal answers at every path are the same closure and the same declaring packages, so
// factsHold reads the same facts and types and decides alike.
func TestTheClosureIndexAnswersAsTheWalks(test *testing.T) {
	test.Parallel()

	candidates, paths := closureFixtureBuild(rand.New(rand.NewPCG(13, 7)))
	paths = append(paths, "example.com/absent")
	index := newClosureIndex(candidates)

	walked := 0
	var walkedLock sync.Mutex
	var group sync.WaitGroup
	for _, candidate := range candidates {
		group.Go(func() {
			// A walk's answer turns on the order it ranges Imports in only when two packages of a path meet in the
			// closure, so the index must walk exactly those, which a walk by package rather than by path finds.
			wantWalked := closureFixtureTwoOfAPath(candidate, false) || closureFixtureTwoOfAPath(candidate, true)
			if walks := index.walks(candidate); walks != wantWalked {
				test.Errorf("%s walks %t, want %t", candidate.ID, walks, wantWalked)
				return
			}
			if wantWalked {
				walkedLock.Lock()
				walked++
				walkedLock.Unlock()
				return
			}
			byPath, closure := closureFixtureOracle(candidate)
			for _, path := range paths {
				if declared := index.declaring(candidate, path); declared != byPath[path] {
					test.Errorf("%s declares %s as %s, the walk as %s", candidate.ID, path, closureFixtureName(declared),
						closureFixtureName(byPath[path]))
				}
				if typed := index.typesOf(candidate, path); typed != closure[path] {
					test.Errorf("%s has other types at %s than load.ClosureTypes", candidate.ID, path)
				}
			}
		})
	}
	group.Wait()
	test.Logf("%d of %d candidates walked, their closures holding two packages of a path", walked, len(candidates))
	if walked == 0 || walked == len(candidates) {
		test.Fatalf("%d of %d candidates walked, want some walked and some read, so both ways are held",
			walked, len(candidates))
	}
}

// TestAClosureHoldingTwoOfAPathIsWalked forces the fallback: a candidate that reaches both "p" and "p [p.test]" is
// walked, as is one whose types-set reach alone holds both, and a candidate beside them is still read from the index.
func TestAClosureHoldingTwoOfAPathIsWalked(test *testing.T) {
	test.Parallel()

	plain := closureFixturePackage("example.com/p", "example.com/p", true)
	variant := closureFixturePackage("example.com/p [example.com/p.test]", "example.com/p", true)
	both := closureFixturePackage("example.com/both", "example.com/both", true, plain, variant)
	untypedVariant := closureFixturePackage("example.com/p [example.com/p.test]", "example.com/p", false)
	// The walk of every import meets two p here, the walk of typed imports only one.
	oneTyped := closureFixturePackage("example.com/one", "example.com/one", true, plain, untypedVariant)
	alone := closureFixturePackage("example.com/alone", "example.com/alone", true, plain)
	index := newClosureIndex([]*packages.Package{both, oneTyped, alone})

	for _, candidate := range []*packages.Package{both, oneTyped} {
		if !index.walks(candidate) {
			test.Errorf("%s is read from the index, want it walked: its closure holds two packages of example.com/p",
				candidate.ID)
		}
	}
	if index.walks(alone) {
		test.Errorf("example.com/alone is walked, want it read from the index: its closure holds one p")
	}
}

// TestAClosureThroughAPackageOfNoTypesHasNoTypesBeyondIt pins load.ClosureTypes's rule the index keeps: an import of
// no types is never gone through, so what only it reaches has no types in the closure, while the walk of every import,
// which declares facts' owners, still reaches it.
func TestAClosureThroughAPackageOfNoTypesHasNoTypesBeyondIt(test *testing.T) {
	test.Parallel()

	deep := closureFixturePackage("example.com/deep", "example.com/deep", true)
	hiding := closureFixturePackage("example.com/hiding", "example.com/hiding", false, deep)
	candidate := closureFixturePackage("example.com/candidate", "example.com/candidate", true, hiding)
	index := newClosureIndex([]*packages.Package{candidate})

	if declared := index.declaring(candidate, "example.com/deep"); declared != deep {
		test.Errorf("example.com/candidate declares example.com/deep as %s, want it", closureFixtureName(declared))
	}
	if index.typesOf(candidate, "example.com/deep") != nil {
		test.Errorf("example.com/candidate has types at example.com/deep, reached only through a package of none")
	}
	if index.typesOf(candidate, "example.com/hiding") != nil {
		test.Errorf("example.com/candidate has types at example.com/hiding, which has none")
	}
}

// TestImportsThatCloseACycleWalkEveryCandidate holds the index to the walks where the load never leaves a cycle but a
// walk would survive one: every candidate is walked.
func TestImportsThatCloseACycleWalkEveryCandidate(test *testing.T) {
	test.Parallel()

	first := closureFixturePackage("example.com/first", "example.com/first", true)
	second := closureFixturePackage("example.com/second", "example.com/second", true, first)
	first.Imports["example.com/second"] = second
	index := newClosureIndex([]*packages.Package{first})

	if !index.walks(first) {
		test.Errorf("example.com/first is read from the index, want it walked: its imports close a cycle")
	}
}

// closureFixturePackage is a package of id at path, of types when typed, importing imports, each written as its ID, so
// a package may import two of one path.
func closureFixturePackage(id string, path string, typed bool, imports ...*packages.Package) *packages.Package {
	made := &packages.Package{ID: id, PkgPath: path, Imports: map[string]*packages.Package{}}
	if typed {
		made.Types = types.NewPackage(path, "fixture")
	}
	for _, imported := range imports {
		made.Imports[imported.ID] = imported
	}
	return made
}

// closureFixtureName is a package's ID, or none.
func closureFixtureName(named *packages.Package) string {
	if named == nil {
		return "none"
	}
	return named.ID
}

// closureFixtureOracle is the walks the closure index replaced, as factsHold made them: the package of each path the
// walk of every import kept, and load.ClosureTypes.
func closureFixtureOracle(candidate *packages.Package) (map[string]*packages.Package, map[string]*types.Package) {
	byPath := map[string]*packages.Package{}
	var collect func(importer *packages.Package)
	collect = func(importer *packages.Package) {
		for _, imported := range importer.Imports {
			if _, collected := byPath[imported.PkgPath]; !collected {
				byPath[imported.PkgPath] = imported
				collect(imported)
			}
		}
	}
	collect(candidate)
	return byPath, load.ClosureTypes(candidate)
}

// closureFixtureTwoOfAPath is whether what candidate reaches, by package, through every import or through those of
// types alone, holds two packages of one path.
func closureFixtureTwoOfAPath(candidate *packages.Package, typedOnly bool) bool {
	reached := map[*packages.Package]bool{}
	var reach func(importer *packages.Package)
	reach = func(importer *packages.Package) {
		for _, imported := range importer.Imports {
			if reached[imported] || (typedOnly && imported.Types == nil) {
				continue
			}
			reached[imported] = true
			reach(imported)
		}
	}
	reach(candidate)
	byPath := map[string]bool{}
	for each := range reached {
		if byPath[each.PkgPath] {
			return true
		}
		byPath[each.PkgPath] = true
	}
	return false
}

// closureFixtureBuild is a build of 80 plain packages, each importing up to four earlier ones, a fifth of them of no
// types; the test variants of every tenth, built as go test builds them, "p [p.test]" and every package reaching p
// again as "q [p.test]" against it, with an external test importing both; and six candidates that import a package and
// a variant of one path, whose closures hold two. It returns every package as a candidate, and every path.
func closureFixtureBuild(random *rand.Rand) ([]*packages.Package, []string) {
	var plain []*packages.Package
	var paths []string
	for number := range 80 {
		path := fmt.Sprintf("example.com/package%d", number)
		paths = append(paths, path)
		made := closureFixturePackage(path, path, random.IntN(5) != 0)
		for range random.IntN(5) {
			if number > 0 {
				imported := plain[random.IntN(number)]
				made.Imports[imported.PkgPath] = imported
			}
		}
		plain = append(plain, made)
	}
	candidates := append([]*packages.Package{}, plain...)
	reaches := func(importer *packages.Package, target *packages.Package) bool {
		seen := map[*packages.Package]bool{}
		var walk func(current *packages.Package) bool
		walk = func(current *packages.Package) bool {
			if current == target {
				return true
			}
			if seen[current] {
				return false
			}
			seen[current] = true
			for _, imported := range current.Imports {
				if walk(imported) {
					return true
				}
			}
			return false
		}
		return walk(importer)
	}
	var variants []*packages.Package
	for rootNumber := 0; rootNumber < len(plain); rootNumber += 10 {
		root := plain[rootNumber]
		suffix := " [" + root.PkgPath + ".test]"
		// In plain's order, imports before importers, so each rebuilt package's imports are rebuilt first.
		rebuilt := map[*packages.Package]*packages.Package{}
		var last *packages.Package
		for _, each := range plain {
			if !reaches(each, root) {
				continue
			}
			again := closureFixturePackage(each.PkgPath+suffix, each.PkgPath, random.IntN(5) != 0)
			for written, imported := range each.Imports {
				if rebuiltImport, isRebuilt := rebuilt[imported]; isRebuilt {
					again.Imports[written] = rebuiltImport
				} else {
					again.Imports[written] = imported
				}
			}
			rebuilt[each] = again
			last = again
			candidates = append(candidates, again)
			variants = append(variants, again)
		}
		// The external tests import p's variant and the last package rebuilt against it, the one most above it.
		externalPath := root.PkgPath + "_test"
		paths = append(paths, externalPath)
		candidates = append(candidates, closureFixturePackage(externalPath+suffix, externalPath, true,
			rebuilt[root], last))
	}
	for number := range 6 {
		variant := variants[random.IntN(len(variants))]
		var plainOfPath *packages.Package
		for _, each := range plain {
			if each.PkgPath == variant.PkgPath {
				plainOfPath = each
			}
		}
		path := fmt.Sprintf("example.com/both%d", number)
		paths = append(paths, path)
		mixed := closureFixturePackage(path, path, true)
		mixed.Imports["plain/"+plainOfPath.PkgPath] = plainOfPath
		mixed.Imports["variant/"+variant.PkgPath] = variant
		candidates = append(candidates, mixed)
	}
	return candidates, paths
}
