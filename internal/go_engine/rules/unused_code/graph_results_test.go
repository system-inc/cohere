package unused_code

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"testing"
)

// unorderedSource is a package whose walk meets its unused objects out of the order they are written in: first's body
// makes third a node before the walk reaches second's declaration.
const unorderedSource = `package unordered

func first() { third() }

type second struct{}

func third() {}
`

// TestUnusedAreInPositionOrder holds the verdict's unused objects, the rule's findings, to the order of their
// positions and not of the graph's nodes (#xeafx79). The nodes are in the order the walk met them, a use before the
// declaration it names, and a build that splices a body's nodes from a record makes them later still, so findings
// in node order would differ between two builds that find the same.
func TestUnusedAreInPositionOrder(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "unordered.go", unorderedSource, 0)
	if parseError != nil {
		test.Fatalf("parsing the package: %v", parseError)
	}
	typesInfo := &types.Info{
		Defs:   map[*ast.Ident]types.Object{},
		Uses:   map[*ast.Ident]types.Object{},
		Scopes: map[ast.Node]*types.Scope{},
		Types:  map[ast.Expr]types.TypeAndValue{},
	}
	files := []*ast.File{file}
	typesPackage, checkError := (&types.Config{}).Check("example.com/unordered", fileSet, files, typesInfo)
	if checkError != nil {
		test.Fatalf("type-checking the package: %v", checkError)
	}
	built := newGraph(fileSet, files, typesPackage, typesInfo, nil, nil)
	built.entry()

	names := []string{}
	for _, each := range built.nodes[1:] {
		names = append(names, each.typesObject.Name())
	}
	if !reflect.DeepEqual(names, []string{"first", "third", "second"}) {
		test.Fatalf("the nodes are %v, in the order written, so the verdict's order proves nothing", names)
	}
	unused := []string{}
	for _, each := range built.results().unused {
		unused = append(unused, each.name)
	}
	if !reflect.DeepEqual(unused, []string{"first", "second", "third"}) {
		test.Errorf("the unused are %v, not in the order of their positions", unused)
	}
}
