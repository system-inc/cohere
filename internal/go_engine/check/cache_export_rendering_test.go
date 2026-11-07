package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// renderingPath is the path of the package each rendering case declares.
const renderingPath = "example.com/rendered"

// checkedInMemory type-checks source as the package at path, its imports among dependencies, each a source of its
// own by path, checked the same way when first imported.
func checkedInMemory(test *testing.T, path string, source string, dependencies map[string]string) *types.Package {
	test.Helper()
	fileSet := token.NewFileSet()
	checked := map[string]*types.Package{}
	var check func(path string, source string) (*types.Package, error)
	check = func(path string, source string) (*types.Package, error) {
		file, parseError := parser.ParseFile(fileSet, path+".go", source, parser.SkipObjectResolution)
		if parseError != nil {
			return nil, parseError
		}
		configuration := types.Config{Importer: importerFunc(func(imported string) (*types.Package, error) {
			if earlier, done := checked[imported]; done {
				return earlier, nil
			}
			dependency, known := dependencies[imported]
			if !known {
				return nil, fmt.Errorf("no package %s", imported)
			}
			return check(imported, dependency)
		})}
		typed, checkError := configuration.Check(path, fileSet, []*ast.File{file}, nil)
		if checkError != nil {
			return nil, checkError
		}
		checked[path] = typed
		return typed, nil
	}
	typed, checkError := check(path, source)
	if checkError != nil {
		test.Fatalf("type-checking %s: %v", path, checkError)
	}
	return typed
}

// renderingCase is two versions of one package, which the export rendering must tell apart, or must not.
type renderingCase struct {
	name         string
	before       string
	after        string
	dependencies map[string]string
}

// fieldOwners are two packages each with a function returning a struct with an unexported field of the same name and
// type, which are two types, since the field's package is part of its identity.
var fieldOwners = map[string]string{
	"example.com/first":  "package first\n\nfunc New() struct{ count int } { return struct{ count int }{} }\n",
	"example.com/second": "package second\n\nfunc New() struct{ count int } { return struct{ count int }{} }\n",
}

// Each pair of packages differs in one thing an importer's type check can read, and their export digests differ:
// every kind of declaration, exported or not, every part of a type TypeString writes, and what the rendering adds
// beside it. A digest that stayed the same over one of these would restore an importer's analysis made against the
// other.
func TestTheExportRenderingTellsApartEveryChangeAnImporterCanRead(test *testing.T) {
	test.Parallel()

	for _, differing := range []renderingCase{
		{name: "a const's value", before: "const C = 1", after: "const C = 2"},
		{name: "a const's type", before: "const C int = 1", after: "const C int64 = 1"},
		{name: "a var's type", before: "var V int", after: "var V uint"},
		{name: "a field's tag", before: "type T struct {\n\tX int `json:\"x\"`\n}",
			after: "type T struct {\n\tX int `json:\"y\"`\n}"},
		{name: "a method added", before: "type T struct{}", after: "type T struct{}\n\nfunc (T) M() {}"},
		{name: "an unexported method on an exported type", before: "type T struct{}",
			after: "type T struct{}\n\nfunc (T) m() {}"},
		{name: "an unexported method's signature", before: "type T struct{}\n\nfunc (T) m(int) {}",
			after: "type T struct{}\n\nfunc (T) m(string) {}"},
		{name: "an unexported field's type", before: "type T struct{ f int }", after: "type T struct{ f string }"},
		{name: "an unexported function's signature", before: "func f(int) {}", after: "func f(string) {}"},
		{name: "a type parameter's constraint", before: "type G[P any] struct{ p P }",
			after: "type G[P comparable] struct{ p P }"},
		{name: "a generic function's constraint", before: "func F[P any](p P) {}",
			after: "func F[P comparable](p P) {}"},
		{name: "an alias's target", before: "type A = int", after: "type A = string"},
		{name: "an alias's type parameter", before: "type A[P any] = []P", after: "type A[P comparable] = []P"},
		{name: "an alias of a defined type", before: "type A = int", after: "type A int"},
		{name: "an embedded field", before: "type U struct{}\n\ntype T struct{ U }",
			after: "type U struct{}\n\ntype T struct{ U U }"},
		{name: "an embedded interface inside an interface",
			before: "type J interface{ M() }\n\ntype I interface{ J }",
			after:  "type J interface{ M() }\n\ntype I interface{ M() }"},
		{name: "variadic", before: "func F(values ...int) {}", after: "func F(values []int) {}"},
		{name: "a result added", before: "func F() {}", after: "func F() int { return 0 }"},
		{name: "a pointer against a value receiver", before: "type T struct{}\n\nfunc (T) M() {}",
			after: "type T struct{}\n\nfunc (*T) M() {}"},
		{name: "the order methods are declared in", before: "type T struct{}\n\nfunc (T) A() {}\n\nfunc (T) B() {}",
			after: "type T struct{}\n\nfunc (T) B() {}\n\nfunc (T) A() {}"},
		{name: "an unexported field's package",
			before:       "import \"example.com/first\"\n\nvar V = first.New()",
			after:        "import \"example.com/second\"\n\nvar V = second.New()",
			dependencies: fieldOwners},
	} {
		test.Run(differing.name, func(test *testing.T) {
			test.Parallel()

			before := checkedInMemory(test, renderingPath, "package rendered\n\n"+differing.before+"\n",
				differing.dependencies)
			after := checkedInMemory(test, renderingPath, "package rendered\n\n"+differing.after+"\n",
				differing.dependencies)
			if exportDigest(before) == exportDigest(after) {
				test.Fatalf("want the digests to differ, both render\n%s", strings.Join(exportRendering(after), "\n"))
			}
		})
	}
}

// What an importer cannot read leaves the export digest as it was: a body, a comment, positions and the order of
// package-level declarations.
func TestTheExportRenderingLeavesOutWhatAnImporterCannotRead(test *testing.T) {
	test.Parallel()

	for _, alike := range []renderingCase{
		{name: "a body", before: "func F() int { return 1 }", after: "func F() int {\n\ttotal := 2\n\treturn total\n}"},
		{name: "a comment that moves every line", before: "type T struct{ X int }",
			after: "// A comment.\n\n// Another.\ntype T struct{ X int }"},
		{name: "the order of declarations", before: "var A int\n\nvar B string", after: "var B string\n\nvar A int"},
		{name: "a local type", before: "func F() {}",
			after: "func F() {\n\ttype local struct{ x int }\n\t_ = local{}\n}"},
	} {
		test.Run(alike.name, func(test *testing.T) {
			test.Parallel()

			before := checkedInMemory(test, renderingPath, "package rendered\n\n"+alike.before+"\n", nil)
			after := checkedInMemory(test, renderingPath, "package rendered\n\n"+alike.after+"\n", nil)
			if exportDigest(before) != exportDigest(after) {
				test.Fatalf("want the digests alike, got\n%s\nagainst\n%s", strings.Join(exportRendering(before), "\n"),
					strings.Join(exportRendering(after), "\n"))
			}
		})
	}
}
