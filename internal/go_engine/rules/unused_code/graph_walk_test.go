package unused_code

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// definedSource is a package whose function declares a function literal's parameter, which go/types records in Defs
// alone, and reads a package-level variable, which it records in Uses alone.
const definedSource = `package defined

var shared = 1

func reader() int {
	_ = func(parameter int) {}
	return shared
}
`

// TestReadResolvesAnIdentifierAsObjectOfDoes holds read's identifier to the object ObjectOf gives it: read asks Uses
// first and ObjectOf only on a miss, so an identifier in Uses alone and one in Defs alone each resolve to their
// object, and the function reading them uses it. The walk itself reads a definition only through ObjectOf, so this
// hands read one directly: a read of Uses alone would find nothing for it.
func TestReadResolvesAnIdentifierAsObjectOfDoes(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "defined.go", definedSource, 0)
	if parseError != nil {
		test.Fatalf("parsing the package: %v", parseError)
	}
	typesInfo := &types.Info{
		Defs:   map[*ast.Ident]types.Object{},
		Uses:   map[*ast.Ident]types.Object{},
		Scopes: map[ast.Node]*types.Scope{},
		Types:  map[ast.Expr]types.TypeAndValue{},
	}
	typesPackage, checkError := (&types.Config{}).Check("example.com/defined", fileSet, []*ast.File{file}, typesInfo)
	if checkError != nil {
		test.Fatalf("type-checking the package: %v", checkError)
	}
	reader := typesPackage.Scope().Lookup("reader")
	identifiers := map[string]*ast.Ident{}
	ast.Inspect(file, func(syntax ast.Node) bool {
		if identifier, isIdentifier := syntax.(*ast.Ident); isIdentifier {
			switch identifier.Name {
			case "parameter":
				identifiers["the definition"] = identifier
			case "shared":
				if _, isUse := typesInfo.Uses[identifier]; isUse {
					identifiers["the use"] = identifier
				}
			}
		}
		return true
	})
	if _, isUse := typesInfo.Uses[identifiers["the definition"]]; isUse {
		test.Fatal("the function literal's parameter is in Uses, so it proves nothing of the fallback")
	}

	for name, identifier := range identifiers {
		built := newGraph(fileSet, []*ast.File{file}, typesPackage, typesInfo, nil, nil)
		built.read(identifier, reader)
		readObject := typesInfo.ObjectOf(identifier)
		if readObject == nil {
			test.Fatalf("%s, %s, resolves to no object", name, identifier.Name)
		}
		if _, isEdge := built.usedEdges[edgeKey(built.node(reader), built.node(readObject))]; !isEdge {
			test.Errorf("reading %s, %s, recorded no use of its object by reader", name, identifier.Name)
		}
	}
	if len(identifiers) != 2 {
		test.Errorf("found %d of the two identifiers the source holds", len(identifiers))
	}
}
