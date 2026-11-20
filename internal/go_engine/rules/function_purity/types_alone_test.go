package function_purity_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
)

// typesAloneSource declares one function of each shape the hook tells apart: an `init`, a function with no results and
// one with a result, each with a body that does work.
const typesAloneSource = `package shapes

var started int

func init() {
	NoResult(1)
}

func NoResult(value int) {
	started = value + 1
}

func Result(value int) int {
	return value + 1
}
`

// Purity names the answers a function's types alone decide (fact_resolver.Kind.TypesAlone): one about a function with
// no results, an `init` among them, which judge answers not pure before it reads the body. An answer about a function
// with a result turns on its body, one about a parameter is no function's, and one about a function with no results
// that pureStandardLibrary lists, were one ever listed, turns on whether its body is a stub.
func TestTheKindNamesTheAnswersItsTypesAloneDecide(test *testing.T) {
	test.Parallel()
	hook := function_purity.Kind.TypesAlone
	if hook == nil {
		test.Fatal("the kind names no answer its types alone decide, so a reader takes every answer as its body's")
	}
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "shapes.go", typesAloneSource, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	shapes, checkError := (&types.Config{}).Check("shapes", fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatal(checkError)
	}
	declared := map[string]*types.Func{}
	for _, object := range info.Defs {
		if function, isFunction := object.(*types.Func); isFunction {
			declared[function.Name()] = function
		}
	}
	for _, name := range []string{"init", "NoResult", "Result"} {
		if declared[name] == nil {
			test.Fatalf("the source declares no function %s", name)
		}
	}
	noResults := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	for _, each := range []struct {
		name   string
		object types.Object
		want   bool
	}{
		{"an init function", declared["init"], true},
		{"a function with no results", declared["NoResult"], true},
		{"a function with a result", declared["Result"], false},
		{"a parameter", declared["Result"].Signature().Params().At(0), false},
		{"a package-level variable", shapes.Scope().Lookup("started"), false},
		{"a listed function, were it to have no results",
			types.NewFunc(token.NoPos, types.NewPackage("strings", "strings"), "TrimSpace", noResults), false},
	} {
		if got := hook(each.object); got != each.want {
			test.Errorf("the hook says %t of %s, want %t", got, each.name, each.want)
		}
	}
}

// typesAloneProbed is what probing a fixture package for the hook found: every source function the hook names, by
// full name, and those of them the resolver answers pure, which is none while the hook speaks for judge.
type typesAloneProbed struct {
	named []string
	pure  []string
}

// The hook never drifts from the judgment it speaks for: over the package's fixtures, every source function the hook
// names is one the resolver answers not pure, as its own package's reader asks. The purity fixture's `init` and its
// NoResult are among them, each with a body judge's walk would allow, so a judge that read a body before it turned a
// function with no results away, or held one pure, answers pure here.
func TestEveryFunctionTheHookNamesIsJudgedNotPure(test *testing.T) {
	test.Parallel()
	hook := function_purity.Kind.TypesAlone
	if hook == nil {
		test.Fatal("the kind names no answer its types alone decide, so there is nothing to hold to judge")
	}
	directory := analysistest.TestData()
	patterns := []string{"purity", "demand/order/after", "demand/order/before", "demand/declares/first",
		"demand/declares/second", "demand/hops/top", "demand/hops/middle", "demand/hops/bottom"}
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	probe := &analysis.Analyzer{
		Name: "types_alone_probe",
		Doc:  "asks the resolver of every source function the purity kind's hook names",
		Requires: []*analysis.Analyzer{function_purity.Analyzer,
			build_intermediate_representation.Analyzer},
		ResultType: reflect.TypeFor[typesAloneProbed](),
		Run: func(pass *analysis.Pass) (any, error) {
			asked, isAsked := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
			if !isAsked {
				return nil, fmt.Errorf("function_purity's result is a %T", pass.ResultOf[function_purity.Analyzer])
			}
			built, readError := build_intermediate_representation.ResultOf(pass)
			if readError != nil {
				return nil, readError
			}
			var found typesAloneProbed
			for function := range built.SourceFunctions() {
				declared, isDeclared := function.Object().(*types.Func)
				if !isDeclared || !hook(declared) {
					continue
				}
				found.named = append(found.named, declared.FullName())
				if asked.IsPure(declared) {
					found.pure = append(found.pure, declared.FullName())
				}
			}
			return found, nil
		},
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, loaded.Roots, check.DispatchOptions())
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	var named []string
	for _, action := range graph.Roots {
		found, isFound := action.Result.(typesAloneProbed)
		if action.Err != nil || !isFound {
			test.Fatalf("probing %s: %v", action.Package.ID, action.Err)
		}
		named = append(named, found.named...)
		for _, pure := range found.pure {
			test.Errorf("%s: the hook says its answer is its types' alone, not pure, and the resolver answers pure",
				pure)
		}
	}
	for _, want := range []string{"purity.init", "purity.NoResult"} {
		if !slices.Contains(named, want) {
			test.Errorf("the hook named %q over the fixtures, want %s among them", named, want)
		}
	}
}
