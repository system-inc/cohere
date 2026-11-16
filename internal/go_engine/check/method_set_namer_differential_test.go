package check

import (
	"fmt"
	"go/constant"
	"go/types"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	release "github.com/system-inc/cohere/internal/release/packaging"
)

// methodSetNamesCorpusVariable names a directory whose packages, `./...` there and every package they import, the
// differential compares in place of std. The Mac compares std; a box compares cohere's and adamic's closures, which
// never load on the Mac (#5xzb4q2):
//
//	COHERE_METHOD_SET_NAMES_CORPUS=<tree> go run ./command/cohere-dev test ./internal/go_engine/check/ \
//	    -run 'TestMethodSetNamesMatchNewMethodSet$' -count=1 -v
const methodSetNamesCorpusVariable = "COHERE_METHOD_SET_NAMES_CORPUS"

// methodSetNamesCorpusMinimumVariable names the fewest sets a corpus must give, a whole number of at least one, one
// when it is unset, so a corpus whose `./...` matches nothing fails rather than passing on no comparison (#jy0v2ef).
const methodSetNamesCorpusMinimumVariable = "COHERE_METHOD_SET_NAMES_CORPUS_MINIMUM"

// methodSetNamesStandardMinimum is fewer sets than std's type names give on any platform the differential loads,
// values and pointers, so it cannot pass on a load that found little: go1.27's std gives 7,919 sets of 4,126 type
// names in 381 packages on darwin/arm64, every package type-checked from source, so every package-scope type name is
// read, unexported ones too.
const methodSetNamesStandardMinimum = 6000

// methodSetNamesReported is how many differing sets the differential names before it stops listing them.
const methodSetNamesReported = 20

// TestMethodSetNamesMatchNewMethodSet holds the namer to NewMethodSet on every type name of std's packages, or of the
// closure methodSetNamesCorpusVariable names: the set of each, and of a pointer to each that is not an interface, both
// as sorted names, since the namer's order is its map's and the method index reads none, while NewMethodSet's is by
// unique name. A name twice, two packages' unexported methods of one spelling, is twice in both.
//
// std is compared for the host and for every platform a release ships besides the host's (release.Targets), each with
// cgo off as a release builds it, so the types only one platform declares, Windows' syscall and
// internal/syscall/windows among them, are compared on any machine (#jy0v2ef). All of them, not a sample, since the
// test tier affords it: on the Mac each platform's std takes about 1 s to type-check from source, and the five beyond
// the host's take about 5 s one at a time and, in parallel with the rest of the package, add at most 2.4 s to its run
// (34.8 s and 32.7 s in two runs, against 32.4 s without them). The six loads at once peak at 2.17 and 2.19 GB RSS
// in two runs, against 0.38 and 0.39 GB for the host's alone (`/usr/bin/time -l` on the test binary `go test -c`
// built, `-test.run` this test).
func TestMethodSetNamesMatchNewMethodSet(test *testing.T) {
	test.Parallel()
	if corpus := os.Getenv(methodSetNamesCorpusVariable); corpus != "" {
		methodSetNamesCompare(test, methodSetNamesLoad(test, corpus, nil, "./..."), methodSetNamesCorpusMinimum(test))
		return
	}
	test.Run("host", func(test *testing.T) {
		test.Parallel()
		loaded := methodSetNamesLoad(test, "", nil, "std")
		methodSetNamesHoldPlatform(test, loaded, runtime.GOOS, runtime.GOARCH)
		methodSetNamesCompare(test, loaded, methodSetNamesStandardMinimum)
	})
	for _, target := range release.Targets {
		// The host's std, loaded above as the host builds it, is this platform's.
		if target.GoOperatingSystem == runtime.GOOS && target.GoArchitecture == runtime.GOARCH {
			continue
		}
		test.Run(target.GoOperatingSystem+"_"+target.GoArchitecture, func(test *testing.T) {
			test.Parallel()
			environment := append(os.Environ(), "GOOS="+target.GoOperatingSystem, "GOARCH="+target.GoArchitecture,
				"CGO_ENABLED=0")
			loaded := methodSetNamesLoad(test, "", environment, "std")
			methodSetNamesHoldPlatform(test, loaded, target.GoOperatingSystem, target.GoArchitecture)
			methodSetNamesCompare(test, loaded, methodSetNamesStandardMinimum)
		})
	}
}

// methodSetNamesHoldPlatform fails unless loaded is std as operatingSystem and architecture build it, read from the
// constants `internal/goos.GOOS` and `internal/goarch.GOARCH` among its packages, so a load that ignored its
// environment and read the host's std cannot pass as another platform's (#jy0v2ef).
func methodSetNamesHoldPlatform(test *testing.T, loaded []*types.Package, operatingSystem string, architecture string) {
	test.Helper()
	if read := methodSetNamesStringConstant(test, loaded, "internal/goos", "GOOS"); read != operatingSystem {
		test.Fatalf("the load is std for GOOS %s, want %s", read, operatingSystem)
	}
	if read := methodSetNamesStringConstant(test, loaded, "internal/goarch", "GOARCH"); read != architecture {
		test.Fatalf("the load is std for GOARCH %s, want %s", read, architecture)
	}
}

// methodSetNamesStringConstant is the value of the string constant name declares in the package of loaded at path.
func methodSetNamesStringConstant(test *testing.T, loaded []*types.Package, path string, name string) string {
	test.Helper()
	for _, each := range loaded {
		if each.Path() != path {
			continue
		}
		declared, isConstant := each.Scope().Lookup(name).(*types.Const)
		if !isConstant || declared.Val().Kind() != constant.String {
			test.Fatalf("%s declares no string constant %s", path, name)
		}
		return constant.StringVal(declared.Val())
	}
	test.Fatalf("the load holds no %s", path)
	return ""
}

// methodSetNamesCompare holds the namer to NewMethodSet on every type name of loaded, failing on any difference and
// on fewer than minimum sets compared, a load that found too little.
func methodSetNamesCompare(test *testing.T, loaded []*types.Package, minimum int) {
	test.Helper()
	namer := newMethodSetNamer()
	compared, typeNames := 0, 0
	var differences []string
	for _, each := range loaded {
		for _, name := range each.Scope().Names() {
			typeName, isTypeName := each.Scope().Lookup(name).(*types.TypeName)
			if !isTypeName {
				continue
			}
			typeNames++
			sets := []types.Type{typeName.Type()}
			if !types.IsInterface(typeName.Type()) {
				sets = append(sets, types.NewPointer(typeName.Type()))
			}
			for _, set := range sets {
				compared++
				got := slices.Sorted(slices.Values(namer.methodNames(set)))
				want := methodSetNamesOf(set)
				if !slices.Equal(got, want) {
					differences = append(differences, fmt.Sprintf("%s.%s, the set of %s: the namer gives %v, "+
						"NewMethodSet %v", each.Path(), name, set, got, want))
				}
			}
		}
	}
	test.Logf("compared %d method sets of %d type names in %d packages", compared, typeNames, len(loaded))
	if len(differences) > 0 {
		shown := differences[:min(len(differences), methodSetNamesReported)]
		test.Fatalf("%d of %d sets differ from NewMethodSet's:\n%s", len(differences), compared,
			strings.Join(shown, "\n"))
	}
	if compared < minimum {
		test.Fatalf("compared %d sets, want at least %d: the load found too little", compared, minimum)
	}
}

// methodSetNamesCorpusMinimum is the fewest sets a corpus must give, methodSetNamesCorpusMinimumVariable's whole
// number, or one when it is unset.
func methodSetNamesCorpusMinimum(test *testing.T) int {
	test.Helper()
	spelled := os.Getenv(methodSetNamesCorpusMinimumVariable)
	if spelled == "" {
		return 1
	}
	minimum, parseError := strconv.Atoi(spelled)
	if parseError != nil || minimum < 1 {
		test.Fatalf("%s is %q, want a whole number of at least 1", methodSetNamesCorpusMinimumVariable, spelled)
	}
	return minimum
}

// methodSetNamesLoad are the types of the packages patterns name from directory, and of every package they import,
// each once, for the platform environment names (the host's when it is nil). Each is type-checked from source,
// function bodies included, since NeedDeps without NeedExportFile reads no export data, so every package-scope type
// name is there, unexported ones too, and every type name's method set reads in full.
func methodSetNamesLoad(test testing.TB, directory string, environment []string, patterns ...string) []*types.Package {
	test.Helper()
	configuration := &packages.Config{
		Dir:  directory,
		Env:  environment,
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps,
	}
	roots, loadError := packages.Load(configuration, patterns...)
	if loadError != nil {
		test.Fatal(loadError)
	}
	var loaded []*types.Package
	failed := 0
	packages.Visit(roots, nil, func(each *packages.Package) {
		if len(each.Errors) > 0 || each.Types == nil {
			failed++
			return
		}
		loaded = append(loaded, each.Types)
	})
	if failed > 0 {
		test.Fatalf("%d packages of %v failed to load", failed, patterns)
	}
	return loaded
}

// BenchmarkMethodSetNames reads every type name of net/http as indexPackage does, a pointer's set for a type that is
// not an interface, both by NewMethodSet and by the namer, one namer a package as indexPackage makes it, so the two
// report their allocations side by side (#5xzb4q2).
func BenchmarkMethodSetNames(benchmark *testing.B) {
	loaded := methodSetNamesLoad(benchmark, "", nil, "net/http")
	var sets []types.Type
	for _, each := range loaded {
		if each.Path() != "net/http" {
			continue
		}
		for _, name := range each.Scope().Names() {
			typeName, isTypeName := each.Scope().Lookup(name).(*types.TypeName)
			if !isTypeName {
				continue
			}
			setOf := typeName.Type()
			if !types.IsInterface(setOf) {
				setOf = types.NewPointer(setOf)
			}
			sets = append(sets, setOf)
		}
	}
	// Both are read once first, so neither times an instance's methods or an interface's type set computed lazily.
	for _, set := range sets {
		methodSetNamesOf(set)
	}
	benchmark.Run("NewMethodSet", func(benchmark *testing.B) {
		benchmark.ReportAllocs()
		read := 0
		for benchmark.Loop() {
			for _, set := range sets {
				methods := types.NewMethodSet(set)
				for index := range methods.Len() {
					read += len(methods.At(index).Obj().Name())
				}
			}
		}
		benchmark.ReportMetric(float64(read)/float64(benchmark.N), "name-bytes/op")
	})
	benchmark.Run("Namer", func(benchmark *testing.B) {
		benchmark.ReportAllocs()
		read := 0
		for benchmark.Loop() {
			namer := newMethodSetNamer()
			for _, set := range sets {
				for _, name := range namer.methodNames(set) {
					read += len(name)
				}
			}
		}
		benchmark.ReportMetric(float64(read)/float64(benchmark.N), "name-bytes/op")
	})
}
