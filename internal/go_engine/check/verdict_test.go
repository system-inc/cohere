package check

import (
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// The pinned fixture's paths: shapes declares the struct a read of another package's object or member asks after,
// closers the interfaces a Method lookup's candidates come from, elsewhere an interface with the same method that no
// package of the user's closure reaches, and kept and fresh two imports of one user, kept restored from the host.
const (
	pinnedShapesPath    = "example.com/pinned/shapes"
	pinnedClosersPath   = "example.com/pinned/closers"
	pinnedElsewherePath = "example.com/pinned/elsewhere"
	pinnedKeptPath      = "example.com/pinned/kept"
	pinnedFreshPath     = "example.com/pinned/fresh"
	pinnedUserPath      = "example.com/pinned/user"
)

// pinnedShapes is shapes as the host declares it: Shape, whose Sides a selection reads.
const pinnedShapes = "package shapes\n\ntype Shape struct{ Sides int }\n\nfunc Count() int { return 1 }\n"

// shapesVerdict is a verdict of a read of the host's shapes in a build whose shapes is buildSource, or which has no
// shapes when buildSource is empty.
func shapesVerdict(test *testing.T, buildSource string) verdict {
	test.Helper()

	closure := map[string]*types.Package{}
	if buildSource != "" {
		closure[pinnedShapesPath] = checkedInMemory(test, pinnedShapesPath, buildSource, nil)
	}
	return verdict{host: NewReadsBuild(), build: NewReadsBuild(), closure: closureLookupOf(closure)}
}

// A read of an object the build cannot find, its package gone or the object gone from it, is declined: with no
// counterpart there is nothing to compare, and a read that held would reuse the host's results for a package whose
// import lost what it read.
func TestAVerdictDeclinesAnObjectTheBuildLacks(test *testing.T) {
	test.Parallel()

	shape := checkedInMemory(test, pinnedShapesPath, pinnedShapes, nil).Scope().Lookup("Shape")
	for _, step := range []struct {
		name  string
		build string
		want  string
	}{
		{"the same shapes", pinnedShapes, ""},
		{"no shapes", "", "object " + pinnedShapesPath + ".Shape: the build has no package " + pinnedShapesPath},
		{"shapes without Shape", "package shapes\n\nfunc Count() int { return 1 }\n",
			"object " + pinnedShapesPath + ".Shape: the build's " + pinnedShapesPath + " has no Shape"},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			holds, why := shapesVerdict(test, step.build).objectHolds(shape)
			if holds != (step.want == "") || why != step.want {
				test.Errorf("want %t, %q, got %t, %q", step.want == "", step.want, holds, why)
			}
		})
	}
}

// A selection through another package's type holds only where the build finds the receiver, finds a member of the
// name on it, and that member's own line is the host's: a receiver gone, a field renamed away, or a field of another
// type each decline it, and judged precisely, no closure digest of the receiver stands behind those checks.
func TestAVerdictSelectionDeclinesAReceiverOrMemberTheBuildLacksOrChanged(test *testing.T) {
	test.Parallel()

	hostShapes := checkedInMemory(test, pinnedShapesPath, pinnedShapes, nil)
	shape, isTypeName := hostShapes.Scope().Lookup("Shape").(*types.TypeName)
	if !isTypeName {
		test.Fatal("the host's Shape is no type name")
	}
	shapeStruct, isStruct := shape.Type().Underlying().(*types.Struct)
	if !isStruct {
		test.Fatalf("the host's Shape is a %T, want a struct", shape.Type().Underlying())
	}
	selection := build_reads.Selection{Receiver: shape, Name: "Sides", Index: []int{0}, Selected: shapeStruct.Field(0)}
	named := "selection " + pinnedShapesPath + ".Shape.Sides: "
	for _, step := range []struct {
		name  string
		build string
		want  string
	}{
		{"the same shapes", pinnedShapes, ""},
		{"no Shape", "package shapes\n\nfunc Count() int { return 1 }\n",
			named + "the build's " + pinnedShapesPath + " has no Shape"},
		{"Sides renamed", "package shapes\n\ntype Shape struct{ Corners int }\n",
			named + "the build finds no one member of the name"},
		{"Sides of another type", "package shapes\n\ntype Shape struct{ Sides string }\n",
			named + "the member it selects differs"},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			holds, why := shapesVerdict(test, step.build).selectionHolds(selection, false)
			if holds != (step.want == "") || why != step.want {
				test.Errorf("want %t, %q, got %t, %q", step.want == "", step.want, holds, why)
			}
		})
	}
}

// Two roots that import one package between them gather every package either reaches: the walk passes over the shared
// one where the second root meets it again and goes on to what comes after it, so a package reached only past a
// shared import is still among the closure's candidates, by its own path. Overlapping imports are the usual case.
func TestAGatheredClosureGoesOnPastAnImportTwoRootsShare(test *testing.T) {
	test.Parallel()

	shared := types.NewPackage("example.com/gathered/shared", "shared")
	first := types.NewPackage("example.com/gathered/first", "first")
	first.SetImports([]*types.Package{shared})
	last := types.NewPackage("example.com/gathered/last", "last")
	second := types.NewPackage("example.com/gathered/second", "second")
	second.SetImports([]*types.Package{shared, last})
	gathered := gatherClosure(NewReadsBuild().closureOf, []*types.Package{first, second})
	want := []*types.Package{first, shared, second, last}
	if !slices.Equal(gathered.ordered, want) {
		test.Errorf("the closure gathered %v, want %v", gathered.ordered, want)
	}
	for _, reached := range want {
		if !gathered.reached[reached] || gathered.byPath[reached.Path()] != reached {
			test.Errorf("the closure did not keep %s by its own path", reached.Path())
		}
	}
	if len(gathered.reached) != len(want) || len(gathered.byPath) != len(want) {
		test.Errorf("the closure holds %d packages and %d paths, want %d of each", len(gathered.reached),
			len(gathered.byPath), len(want))
	}
}

// pinnedEmbeddingUser is the user package whose Mine embeds closers' Base, which keeps its Method lookup of Close on
// the closure judgment: every type name of either closure with Close in its method set, compared one by one.
const pinnedEmbeddingUser = "package user\n\nimport \"example.com/pinned/closers\"\n\n" +
	"type Mine struct{ closers.Base }\n\nfunc (Mine) Close() error { return nil }\n"

// pinnedElsewhere is a package of an interface with Close that the user's closure never reaches.
const pinnedElsewhere = "package elsewhere\n\ntype Shut interface{ Close() error }\n"

// A Method lookup kept on the closure judgment compares every type name with the method on each side, so it declines a
// build that lost one, gained one, or changed the last of them, however the memos' indexes hold other packages: each
// memo indexed elsewhere first, as an earlier verdict of another package does, so elsewhere's entry for Close comes
// before closers' in both and is passed over, never the end of the candidates.
func TestTheClosureJudgmentComparesEveryCandidatePastAnotherClosuresEntry(test *testing.T) {
	test.Parallel()

	base := "package closers\n\ntype Base struct{}\n"
	for _, step := range []struct {
		name  string
		host  string
		build string
		want  string
	}{
		{"the same closers", base + "\ntype Closer interface{ Close() error }\n",
			base + "\ntype Closer interface{ Close() error }\n", ""},
		{"a candidate the build lost", base + "\ntype Closer interface{ Close() error }\n", base,
			"L lookup Method Close: 1 type names have the method on the host, 0 in the build"},
		{"a candidate the build gained", base, base + "\ntype Closer interface{ Close() error }\n",
			"L lookup Method Close: 0 type names have the method on the host, 1 in the build"},
		{"the last candidate changed",
			base + "\ntype First interface{ Close() error }\n\ntype Second interface{ Close() error }\n",
			base + "\ntype First interface{ Close() error }\n\n" +
				"type Second interface {\n\tClose() error\n\tFlush()\n}\n",
			"L lookup Method Close: " + pinnedClosersPath + ".Second differs in the build"},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			hostUser := checkedInMemory(test, pinnedUserPath, pinnedEmbeddingUser,
				map[string]string{pinnedClosersPath: step.host})
			buildClosers := checkedInMemory(test, pinnedClosersPath, step.build, nil)
			host, build := NewReadsBuild(), NewReadsBuild()
			hostElsewhere := checkedInMemory(test, pinnedElsewherePath, pinnedElsewhere, nil)
			buildElsewhere := checkedInMemory(test, pinnedElsewherePath, pinnedElsewhere, nil)
			host.indexPackage(hostElsewhere)
			build.indexPackage(buildElsewhere)
			reads := &build_reads.Reads{}
			reads.RecordTypes(hostUser, &types.Info{}, nil)
			reads.AddLookup(askedBy(build_reads.Lookup{Query: build_reads.QueryMethod, Name: "Close",
				Subject: methodOf(hostUser, "Mine", "Close")}))
			restoring := Restoring{Path: pinnedUserPath, Imports: []string{pinnedClosersPath}, Reads: reads}
			holds, why := TypesHold(restoring, map[string]*types.Package{pinnedClosersPath: buildClosers}, nil, host,
				build)
			if holds != (step.want == "") || why != step.want {
				test.Errorf("want %t, %q, got %t, %q", step.want == "", step.want, holds, why)
			}
			for side, memo := range map[string]*ReadsBuild{"host's": host, "build's": build} {
				indexed := memo.methodsNamed("Close")
				if len(indexed) == 0 || indexed[0].owner.Path() != pinnedElsewherePath {
					test.Errorf("the %s index of Close does not begin with elsewhere, so the step tells nothing", side)
				}
			}
		})
	}
}

// A package the build restored from the host, which the host's side stands for, is passed over when the build indexes
// its closure, and every package after it is still indexed: fresh, an import after kept, gains an interface whose
// Close has the subject's shape, which flips the rule's bit, so the lookup is declined.
func TestTheBuildIndexesThePackagesPastARestoredOne(test *testing.T) {
	test.Parallel()

	kept := "package kept\n\nfunc Count() int { return 1 }\n"
	fresh := "package fresh\n\nfunc Count() int { return 1 }\n"
	hostUser := checkedInMemory(test, pinnedUserPath, "package user\n\nimport (\n\t\"example.com/pinned/kept\"\n"+
		"\t\"example.com/pinned/fresh\"\n)\n\ntype Mine struct{}\n\nfunc (Mine) Close() error { return nil }\n\n"+
		"func Use() int { return kept.Count() + fresh.Count() }\n",
		map[string]string{pinnedKeptPath: kept, pinnedFreshPath: fresh})
	buildKept := checkedInMemory(test, pinnedKeptPath, kept, nil)
	buildFresh := checkedInMemory(test, pinnedFreshPath, fresh+"\ntype Closer interface{ Close() error }\n", nil)
	reads := &build_reads.Reads{}
	reads.RecordTypes(hostUser, &types.Info{}, nil)
	reads.AddLookup(askedBy(build_reads.Lookup{Query: build_reads.QueryMethod, Name: "Close",
		Subject: methodOf(hostUser, "Mine", "Close")}))
	restoring := Restoring{Path: pinnedUserPath, Imports: []string{pinnedKeptPath, pinnedFreshPath}, Reads: reads}
	closure := map[string]*types.Package{pinnedKeptPath: buildKept, pinnedFreshPath: buildFresh}
	build := NewReadsBuild()
	indexed := map[*types.Package]int{}
	build.indexing = func(declaring *types.Package) { indexed[declaring]++ }
	holds, why := TypesHold(restoring, closure, map[*types.Package]bool{buildKept: true}, NewReadsBuild(), build)
	want := "L lookup Method Close: " + pinnedFreshPath + ".Closer.Close is new in the build, of the subject's shape"
	if holds || why != want {
		test.Errorf("want false, %q, got %t, %q", want, holds, why)
	}
	if indexed[buildKept] != 0 || indexed[buildFresh] != 1 {
		test.Errorf("the build indexed kept %d times and fresh %d, want never and once", indexed[buildKept],
			indexed[buildFresh])
	}
}

// Two objects are one object in each build only when both are of one package path at one objectpath, rendered alike:
// the universe's against a package's, two packages' of one name, two fields alike in all but the variable holding
// them, and an object with no objectpath are each other objects, while one object is itself, the universe's too.
func TestSameObjectIsOnePathAndOneObjectpathRenderedAlike(test *testing.T) {
	test.Parallel()

	source := "package shapes\n\ntype Shape struct{ Sides int }\n\nvar Wide struct{ Span int }\n\n" +
		"var Tall struct{ Span int }\n"
	hostShapes := checkedInMemory(test, pinnedShapesPath, source, nil)
	buildShapes := checkedInMemory(test, pinnedShapesPath, source, nil)
	otherShapes := checkedInMemory(test, "example.com/pinned/other", source, nil)
	span := func(shapes *types.Package, variable string) types.Object {
		holding, isStruct := shapes.Scope().Lookup(variable).Type().(*types.Struct)
		if !isStruct {
			test.Fatalf("%s is a %T, want a struct", variable, shapes.Scope().Lookup(variable).Type())
		}
		return holding.Field(0)
	}
	unplaced := types.NewVar(token.NoPos, hostShapes, "Shape", hostShapes.Scope().Lookup("Shape").Type())
	errorType := types.Universe.Lookup("error")
	for _, step := range []struct {
		name   string
		host   types.Object
		build  types.Object
		wanted bool
	}{
		{"the universe's error, itself", errorType, errorType, true},
		{"Shape in each build", hostShapes.Scope().Lookup("Shape"), buildShapes.Scope().Lookup("Shape"), true},
		{"the universe's error against Shape", errorType, buildShapes.Scope().Lookup("Shape"), false},
		{"Shape against the universe's error", hostShapes.Scope().Lookup("Shape"), errorType, false},
		{"Shape against another package's", hostShapes.Scope().Lookup("Shape"), otherShapes.Scope().Lookup("Shape"),
			false},
		{"Wide's Span against Tall's", span(hostShapes, "Wide"), span(buildShapes, "Tall"), false},
		{"a variable with no objectpath against Shape", unplaced, buildShapes.Scope().Lookup("Shape"), false},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			judged := verdict{host: NewReadsBuild(), build: NewReadsBuild()}
			if same := judged.sameObject(step.host, step.build); same != step.wanted {
				test.Errorf("sameObject gave %t, want %t", same, step.wanted)
			}
		})
	}
}
