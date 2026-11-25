package unused_code

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"testing"

	"honnef.co/go/tools/analysis/facts/generated"
)

// fileFreeSource is a package whose walk needs no file's name or line: locals written in loops, by assignment and by
// increment, a field written, a method, and a package-level variable read but never written, with no constant, whose
// groups are read by line, and no generated file.
const fileFreeSource = `package walked

type counter struct {
	count int
	step  int
}

func (each *counter) add(times int) int {
	total := 0
	for index := 0; index < times; index++ {
		total += each.step
		each.count++
	}
	for _, value := range []int{1, 2} {
		total = total + value
	}
	return total
}

var shared = counter{step: 1}

func Exported() int {
	return shared.add(3)
}

func unused() {}
`

// TestEntryAsksNoFileWhereNoFileDecides holds entry and the walk to asking the shared file set only for a position
// that decides something (#fex6s0v): each object's file only when the package has a generated file, and a written
// variable's file only when the variable is package-level, the only kind a write in a test uses. Each ask reads, and
// often writes, the file set's cache of its last file, shared by every core. The graph is built once with the
// package's file set and once with none, where any ask would panic, and both verdicts must be the same.
func TestEntryAsksNoFileWhereNoFileDecides(test *testing.T) {
	test.Parallel()
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "walked.go", fileFreeSource, 0)
	if parseError != nil {
		test.Fatalf("parsing the package: %v", parseError)
	}
	typesInfo := &types.Info{
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Scopes:     map[ast.Node]*types.Scope{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Types:      map[ast.Expr]types.TypeAndValue{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	files := []*ast.File{file}
	typesPackage, checkError := (&types.Config{}).Check("example.com/walked", fileSet, files, typesInfo)
	if checkError != nil {
		test.Fatalf("type-checking the package: %v", checkError)
	}
	build := func(graphFileSet *token.FileSet) (verdict result, panicked any) {
		defer func() { panicked = recover() }()
		built := newGraph(graphFileSet, files, typesPackage, typesInfo, nil, map[string]generated.Generator{})
		if entryError := built.entry(); entryError != nil {
			test.Fatalf("building the graph: %v", entryError)
		}
		return built.results(), nil
	}

	expected, panicked := build(fileSet)
	if panicked != nil {
		test.Fatalf("building the graph with the package's file set panicked: %v", panicked)
	}
	if len(expected.used) == 0 || len(expected.unused) == 0 {
		test.Fatalf("the verdict names %d used and %d unused, where the package has both", len(expected.used),
			len(expected.unused))
	}
	found, panicked := build(nil)
	if panicked != nil {
		test.Fatalf("building the graph asked the file set for a position that decides nothing: %v", panicked)
	}
	if !reflect.DeepEqual(found, expected) {
		test.Errorf("without the file set the verdict is %+v, with it %+v", found, expected)
	}
}
