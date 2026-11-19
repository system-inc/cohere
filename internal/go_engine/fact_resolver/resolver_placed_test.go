package fact_resolver_test

import (
	"bytes"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

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
// bytes. Method's receiver, no parameter, is left out.
func TestPlacedInPlacesEveryAnswerAtItsDeclarationsFunc(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"placed"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
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
		test.Fatal("the resolver never learned placed, which ctrlflow ran on")
	}
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
	if placed := placedNames(resolver.PlacedIn(home)); !slices.Equal(placed, want) {
		test.Errorf("want placed\n%q\ngot\n%q", want, placed)
	}
}
