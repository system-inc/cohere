package intermediate_representation_test

// Building one function builds only it (#6pt3bt8, wave 3b), and what laziness saves, measured.

import (
	"math/rand/v2"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// TestBuildingOneFunctionBuildsOnlyIt builds strings.Title, which holds a function literal, from a package nothing
// else of has been built, and holds every other function of the package unbuilt. A Build that built the whole
// package fails it.
func TestBuildingOneFunctionBuildsOnlyIt(test *testing.T) {
	test.Parallel()
	loaded, loadError := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedFiles |
		packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
		packages.NeedImports}, "strings")
	if loadError != nil || len(loaded) != 1 || len(loaded[0].Errors) > 0 {
		test.Fatalf("loading strings: %v %v", loadError, loaded)
	}
	unit := differentialUnit{path: loaded[0].PkgPath, fileSet: loaded[0].Fset, types: loaded[0].Types,
		files: loaded[0].Syntax, typeInfo: loaded[0].TypesInfo}
	program := intermediate_representation.NewProgram(unit.fileSet)
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	title := built.Function("Title")
	title.Build()

	if title.Blocks == nil {
		test.Fatal("strings.Title has no blocks after Build")
	}
	if len(title.AnonymousFunctions) == 0 {
		test.Fatal("strings.Title holds no anonymous function, so the test doesn't hold one to being built with it")
	}
	for _, anonymous := range title.AnonymousFunctions {
		if anonymous.Blocks == nil {
			test.Errorf("%s, declared in strings.Title, wasn't built with it", anonymous.RelativeString(nil))
		}
	}
	others := 0
	for _, function := range built.Functions {
		if function == title {
			continue
		}
		others++
		if function.Blocks != nil {
			test.Errorf("%s was built, though only strings.Title was asked for", function.RelativeString(nil))
		}
	}
	if others < 50 {
		test.Errorf("strings holds %d other functions, too few for the test to mean much", others)
	}
}

// TestLazyBuildCost builds the corpus COHERE_GO_IR_DIFFERENTIAL names (the ordinary one when unset) three ways,
// one after another, and logs each build's time, the bytes it allocated and the bytes it holds once built: every
// function eagerly with Package.Build, the packages created and no function built, and a seeded tenth of each
// package's functions with Function.Build. The second is the part laziness can't save: each program creates every
// import's members from type information. Run it alone for numbers.
func TestLazyBuildCost(test *testing.T) {
	test.Parallel()
	corpus := os.Getenv(differentialCorpusVariable)
	units := differentialCorpusUnits(test, corpus)
	// One build first, unmeasured, so the caches go/types fills on first use are no build's cost.
	for _, unit := range units {
		differentialBuildOurs(unit, false)
	}
	for _, build := range []struct {
		name  string
		build func(unit differentialUnit) *intermediate_representation.Package
	}{
		{"every function, Package.Build", func(unit differentialUnit) *intermediate_representation.Package {
			return differentialBuildOurs(unit, false)
		}},
		{"no function, CreatePackage alone", func(unit differentialUnit) *intermediate_representation.Package {
			return differentialCreateOurs(unit)
		}},
		{"a tenth of the functions, Function.Build, seed 1", func(
			unit differentialUnit,
		) *intermediate_representation.Package {
			return differentialBuildTenth(unit)
		}},
	} {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		started := time.Now()
		built := make([]*intermediate_representation.Package, 0, len(units))
		for _, unit := range units {
			built = append(built, build.build(unit))
		}
		elapsed := time.Since(started)
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		functions := 0
		for _, each := range built {
			for _, function := range each.Functions {
				if function.Blocks != nil {
					functions++
				}
			}
		}
		runtime.KeepAlive(built)
		test.Logf("cost, %s, corpus %s: %d packages, %d functions built in %s, %.1f MiB allocated, %.1f MiB held after",
			build.name, differentialCorpusName(corpus), len(units), functions, elapsed.Round(time.Millisecond),
			float64(after.TotalAlloc-before.TotalAlloc)/(1<<20),
			(float64(after.HeapAlloc)-float64(before.HeapAlloc))/(1<<20))
	}
}

// differentialCreateOurs creates unit's package and its imports' in a program of their own, building nothing.
func differentialCreateOurs(unit differentialUnit) *intermediate_representation.Package {
	program := intermediate_representation.NewProgram(unit.fileSet)
	program.SetNoReturn(differentialNoReturn)
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	return program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
}

// differentialBuildTenth creates unit's package and builds a seeded tenth of its functions, at least one, with
// Function.Build.
func differentialBuildTenth(unit differentialUnit) *intermediate_representation.Package {
	built := differentialCreateOurs(unit)
	functions := slices.Clone(built.Functions)
	rand.New(rand.NewPCG(1, 1)).Shuffle(len(functions), func(left int, right int) {
		functions[left], functions[right] = functions[right], functions[left]
	})
	for _, function := range functions[:max(1, len(functions)/10)] {
		function.Build()
	}
	return built
}
