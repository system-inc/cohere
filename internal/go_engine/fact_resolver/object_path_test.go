package fact_resolver_test

import (
	"go/types"
	"testing"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// reachedObjects is every object reached from the scope of each package in packages: its members, and through their
// types every method, interface method, struct field, parameter, result, receiver and type parameter, each once.
func reachedObjects(packages []*types.Package) []types.Object {
	var reached []types.Object
	seenObjects := map[types.Object]bool{}
	seenTypes := map[types.Type]bool{}
	var visitType func(visited types.Type)
	visitObject := func(object types.Object) {
		if object == nil || seenObjects[object] {
			return
		}
		seenObjects[object] = true
		reached = append(reached, object)
		visitType(object.Type())
	}
	visitTuple := func(tuple *types.Tuple) {
		for variable := range tuple.Variables() {
			visitObject(variable)
		}
	}
	visitTypeParameters := func(parameters *types.TypeParamList) {
		for parameter := range parameters.TypeParams() {
			visitObject(parameter.Obj())
		}
	}
	visitType = func(visited types.Type) {
		if visited == nil || seenTypes[visited] {
			return
		}
		seenTypes[visited] = true
		switch typed := visited.(type) {
		case *types.Named:
			visitObject(typed.Obj())
			visitTypeParameters(typed.TypeParams())
			for method := range typed.Methods() {
				visitObject(method)
			}
			visitType(typed.Underlying())
		case *types.Alias:
			visitObject(typed.Obj())
			visitType(typed.Rhs())
		case *types.Struct:
			for field := range typed.Fields() {
				visitObject(field)
			}
		case *types.Interface:
			for method := range typed.ExplicitMethods() {
				visitObject(method)
			}
			for embedded := range typed.EmbeddedTypes() {
				visitType(embedded)
			}
		case *types.Signature:
			if receiver := typed.Recv(); receiver != nil {
				visitObject(receiver)
			}
			visitTypeParameters(typed.TypeParams())
			visitTuple(typed.Params())
			visitTuple(typed.Results())
		case *types.Pointer:
			visitType(typed.Elem())
		case *types.Slice:
			visitType(typed.Elem())
		case *types.Array:
			visitType(typed.Elem())
		case *types.Map:
			visitType(typed.Key())
			visitType(typed.Elem())
		case *types.Chan:
			visitType(typed.Elem())
		case *types.TypeParam:
			visitObject(typed.Obj())
			visitType(typed.Constraint())
		case *types.Union:
			for term := range typed.Terms() {
				visitType(term.Type())
			}
		}
	}
	for _, scoped := range packages {
		for _, name := range scoped.Scope().Names() {
			visitObject(scoped.Scope().Lookup(name))
		}
	}
	return reached
}

// The resolver's encoders name every object exactly as objectpath.For does, the same path or the same lack of one,
// over every object reached from the scopes of the bodiless fixture and of go/ast, a package of named types, methods,
// interfaces, struct fields and generic functions: a path is an answer's key in the store, so one the encoders gave
// otherwise would key the same answer apart in two runs. Each object is asked twice, since an encoder searches its
// package the first time and answers from its index after.
func TestTheResolverNamesEveryObjectAsObjectpathDoes(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"bodiless/middle", "go/ast"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	var packages []*types.Package
	for _, root := range loaded.Roots {
		packages = append(packages, root.Types)
		for _, imported := range root.Imports {
			packages = append(packages, imported.Types)
		}
	}
	reached := reachedObjects(packages)
	resolver := fact_resolver.New(nil)
	named := 0
	for round := range 2 {
		for _, object := range reached {
			wanted, wantedError := objectpath.For(object)
			got, gotError := resolver.ObjectPath(object)
			if got != wanted || (gotError == nil) != (wantedError == nil) {
				test.Fatalf("round %d, %s: want path %q, error %v, got %q, error %v", round, object, wanted,
					wantedError, got, gotError)
			}
			if round == 0 && wantedError == nil {
				named++
			}
		}
	}
	// A floor, so a walk that reached too little to test anything fails rather than passing on nothing.
	if named < 1000 {
		test.Fatalf("want at least 1000 objects with a path among the %d reached, got %d", len(reached), named)
	}
}
