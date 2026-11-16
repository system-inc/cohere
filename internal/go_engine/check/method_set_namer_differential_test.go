package check

import (
	"fmt"
	"go/types"
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// methodSetNamesCorpusVariable names a directory whose packages, `./...` there and every package they import, the
// differential compares in place of std. The Mac compares std; a box compares cohere's and adamic's closures, which
// never load on the Mac (#5xzb4q2):
//
//	COHERE_METHOD_SET_NAMES_CORPUS=<tree> go run ./command/cohere-dev test ./internal/go_engine/check/ \
//	    -run 'TestMethodSetNamesMatchNewMethodSet$' -count=1 -v
const methodSetNamesCorpusVariable = "COHERE_METHOD_SET_NAMES_CORPUS"

// methodSetNamesStandardMinimum is fewer sets than std's type names give, values and pointers, so the differential
// cannot pass on a load that found little: go1.27's std gives 7,919 sets of 4,126 type names in 381 packages, its
// export data holding every package-scope type name, unexported ones too.
const methodSetNamesStandardMinimum = 6000

// methodSetNamesReported is how many differing sets the differential names before it stops listing them.
const methodSetNamesReported = 20

// TestMethodSetNamesMatchNewMethodSet holds the namer to NewMethodSet on every type name of std's packages, or of
// the closure methodSetNamesCorpusVariable names: the set of each, and of a pointer to each that is not an interface,
// both as sorted names, since the namer's order is its map's and the method index reads none, while NewMethodSet's is
// by unique name. A name twice, two packages' unexported methods of one spelling, is twice in both.
func TestMethodSetNamesMatchNewMethodSet(test *testing.T) {
	test.Parallel()
	directory, patterns := "", []string{"std"}
	corpus := os.Getenv(methodSetNamesCorpusVariable)
	if corpus != "" {
		directory, patterns = corpus, []string{"./..."}
	}
	loaded := methodSetNamesLoad(test, directory, patterns)
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
	if corpus == "" && compared < methodSetNamesStandardMinimum {
		test.Fatalf("compared %d sets of std, want at least %d: the load found too little", compared,
			methodSetNamesStandardMinimum)
	}
}

// methodSetNamesLoad are the types of the packages patterns name from directory, and of every package they import,
// each once, from export data, which every type name's method set reads in full.
func methodSetNamesLoad(test testing.TB, directory string, patterns []string) []*types.Package {
	test.Helper()
	configuration := &packages.Config{
		Dir:  directory,
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
	loaded := methodSetNamesLoad(benchmark, "", []string{"net/http"})
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
