package build_dependence

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

// A struct whose field is named twice is a type error, and the type checker records the struct with the name once, so
// its syntax holds more fields than its type: the collector declares the fields its type holds and stops at the
// syntax's surplus (collectFields), so a graph handed such a package, which the engine never hands it (#jzzdvcf), adds
// it without panicking and declares Shape. Index the type's fields by the syntax's alone and the add panics.
func TestAGraphAddsAStructWithADuplicateFieldName(test *testing.T) {
	test.Parallel()

	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "broken.go",
		"package broken\n\ntype Shape struct {\n\ta int\n\ta string\n}\n", 0)
	if parseError != nil {
		test.Fatal(parseError)
	}
	information := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{},
		Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
		Instances: map[*ast.Ident]types.Instance{}, Implicits: map[ast.Node]types.Object{}}
	// An Error func has the checker go on past the duplicate field, as the engine's load does, and record the rest.
	var typeErrors []error
	configuration := types.Config{Error: func(typeError error) { typeErrors = append(typeErrors, typeError) }}
	checked, checkError := configuration.Check("example.com/broken", fileSet, []*ast.File{file}, information)
	if checkError == nil || len(typeErrors) == 0 {
		test.Fatal("the duplicate field checked with no type error")
	}
	graph := NewGraph()
	graph.AddPackage(&packages.Package{ID: "broken", PkgPath: "example.com/broken", Fset: fileSet,
		Syntax: []*ast.File{file}, Types: checked, TypesInfo: information, IllTyped: true}, true, nil)
	if _, isDeclared := graph.Against(Difference{}).NameBits("example.com/broken", "Shape"); !isDeclared {
		test.Error("the graph does not declare Shape")
	}
}
