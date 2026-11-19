package fact_resolver_test

import (
	"bytes"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// noDigests is a PlacedIn digestOf that holds no file's digest.
func noDigests(string) ([]byte, bool) {
	return nil, false
}

// learnPlacedFixture is a resolver that learned the fixture package at path, under fact_resolver's testdata, as
// ctrlflow finishing on it teaches one, the package as it holds it, and as the load gave it.
func learnPlacedFixture(test *testing.T, path string) (*fact_resolver.Resolver, *fact_resolver.Package,
	*packages.Package) {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{path}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil || len(loaded.Roots) != 1 {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	resolver := fact_resolver.New(nil)
	options := check.DispatchOptions()
	options.Resolver = resolver
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{ctrlflow.Analyzer}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running ctrlflow on %s: %v", action.Package.ID, action.Err)
		}
	}
	home := resolver.Package(loaded.Roots[0].Types)
	if home == nil {
		test.Fatalf("the resolver never learned %s, which ctrlflow ran on", path)
	}
	return resolver, home, loaded.Roots[0]
}

// placedNames is placed as a test compares it, each answer as its kind, file, offset, parameter and answer on one line,
// in the order they came.
func placedNames(placed []fact_resolver.PlacedAnswer) []string {
	var named []string
	for _, answer := range placed {
		named = append(named, fmt.Sprintf("%s %s %d %d %s", answer.Kind, answer.File, answer.Offset,
			answer.Parameter, answer.Encoded))
	}
	return named
}

// PlacedIn places every answer the run holds about placed's functions and their parameters at the function's `func`,
// as a build's unit is placed, read from the fixture's bytes here rather than from its syntax: hidden's, which
// objectpath cannot name, as Exported's; Exported's own, which a walk kept and no question asked of, as a memoized one
// is; a parameter's at its index, Method's second at 1, its receiver not counted; and directed's in written.go, the
// file the //line directive above it names, at its offset in placed.go, where the directive moves its line and not its
// bytes, carrying placed.go's digest, the bytes its offset counts in. Method's receiver, no parameter, is left out.
func TestPlacedInPlacesEveryAnswerAtItsFunctionsKeyword(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	resolver, home, _ := learnPlacedFixture(test, "placed")
	scope := home.Types.Scope()
	function := func(name string) *types.Func {
		declared, isFunction := scope.Lookup(name).(*types.Func)
		if !isFunction {
			test.Fatalf("placed declares no function %s", name)
		}
		return declared
	}
	holder, isNamed := scope.Lookup("Holder").Type().(*types.Named)
	if !isNamed || holder.NumMethods() != 1 {
		test.Fatal("placed declares no named type Holder with one method")
	}
	method := holder.Method(0)
	ask := func(object types.Object, answer bool) {
		fact_resolver.Ask(resolver, home, keepProbe, object, func(*fact_resolver.Package) bool { return answer },
			func() bool { return false })
	}
	ask(function("hidden"), true)
	ask(function("Exported").Signature().Params().At(0), false)
	ask(method.Signature().Params().At(1), true)
	ask(method.Signature().Recv(), true)
	ask(function("directed"), false)
	fact_resolver.Keep(resolver, home, keepProbe, function("Exported"), true)

	path := filepath.Join(directory, "src", "placed", "placed.go")
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatalf("reading the fixture: %v", readError)
	}
	offsetOf := func(declaration string) int {
		offset := bytes.Index(contents, []byte(declaration))
		if offset < 0 {
			test.Fatalf("placed.go holds no %q", declaration)
		}
		return offset
	}
	written := filepath.Join(filepath.Dir(path), "written.go")
	want := []string{
		fmt.Sprintf("keep_probe %s %d -1 true", path, offsetOf("func Exported(")),
		fmt.Sprintf("keep_probe %s %d 0 false", path, offsetOf("func Exported(")),
		fmt.Sprintf("keep_probe %s %d -1 true", path, offsetOf("func hidden(")),
		fmt.Sprintf("keep_probe %s %d 1 true", path, offsetOf("func (holder Holder) Method(")),
		fmt.Sprintf("keep_probe %s %d -1 false", written, offsetOf("func directed(")),
	}
	// Each answer carries the digest of the file it is parsed from, placed.go for directed too, whose answer is placed
	// in written.go: its offset counts in placed.go's bytes.
	placedAnswers := resolver.PlacedIn(home, func(parsed string) ([]byte, bool) {
		return []byte("digest of " + filepath.Base(parsed)), true
	})
	if placed := placedNames(placedAnswers); !slices.Equal(placed, want) {
		test.Errorf("want placed\n%q\ngot\n%q", want, placed)
	}
	for _, answer := range placedAnswers {
		if string(answer.FileDigest) != "digest of placed.go" {
			test.Errorf("want every answer to carry placed.go's digest, got %q at %s:%d", answer.FileDigest,
				filepath.Base(answer.File), answer.Offset)
		}
	}
}

// A place two declared functions share names neither, so it is never matched (#k7k3wwc): cmd/cgo writes
// `//line <user file>:N` inside _cgo_gotypes.go, so a generated function can share its file's name and offset with the
// user's own in another file. one and two, in two files, sit after `//line shared.go:1` at one offset, and are left
// out of the places, their parameters too, and so of PlacedIn, while alone, after the same directive at an offset of
// its own, is placed with its parameter.
func TestAPlaceTwoFunctionsShareIsNoPlace(test *testing.T) {
	test.Parallel()
	resolver, home, loaded := learnPlacedFixture(test, "collide")
	scope := home.Types.Scope()
	var named []string
	for object, place := range fact_resolver.DeclarationPlaces(loaded.Fset, loaded.Syntax, loaded.TypesInfo) {
		named = append(named, fmt.Sprintf("%s %s %d", object.Name(), filepath.Base(place.File), place.Parameter))
		fact_resolver.Ask(resolver, home, keepProbe, object, func(*fact_resolver.Package) bool { return true },
			func() bool { return false })
	}
	for _, name := range []string{"one", "two"} {
		fact_resolver.Ask(resolver, home, keepProbe, scope.Lookup(name), func(*fact_resolver.Package) bool {
			return true
		}, func() bool { return false })
	}
	slices.Sort(named)
	if want := []string{"alone shared.go -1", "count shared.go 0"}; !slices.Equal(named, want) {
		test.Errorf("want only %q placed, got %q", want, named)
	}
	if placed := resolver.PlacedIn(home, noDigests); len(placed) != 2 {
		test.Errorf("want alone's two answers placed and none of one's or two's, got %+v", placed)
	}
}

// ComparePlaced orders by file, the file's digest, offset, parameter, kind and the answer's bytes, in that order: at
// one place, a function's own answer of a later kind comes before its parameter's of an earlier one, and the answer in
// the file whose digest sorts first before the one in the other file of that name.
func TestComparePlacedOrdersByParameterBeforeKind(test *testing.T) {
	test.Parallel()
	parameter := fact_resolver.PlacedAnswer{Kind: "function_purity", File: "a.go", FileDigest: []byte("1"), Offset: 9,
		Parameter: 0, Encoded: []byte("true")}
	function := fact_resolver.PlacedAnswer{Kind: "return_nil_ness", File: "a.go", FileDigest: []byte("1"), Offset: 9,
		Parameter: -1, Encoded: []byte("[]")}
	other := fact_resolver.PlacedAnswer{Kind: "function_purity", File: "a.go", FileDigest: []byte("2"), Offset: 1,
		Parameter: -1, Encoded: []byte("true")}
	sorted := slices.SortedFunc(slices.Values([]fact_resolver.PlacedAnswer{other, parameter, function}),
		fact_resolver.ComparePlaced)
	want := []fact_resolver.PlacedAnswer{function, parameter, other}
	if !reflect.DeepEqual(sorted, want) {
		test.Errorf("want %+v, got %+v", want, sorted)
	}
}
