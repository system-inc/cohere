package export_data

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/system-inc/go-types/types"
)

// The packages the reads below write and read: user's API names dependency's Thing, in a variable's type, a
// function's result and an embedded field, so user's shallow export data names dependency and reads only against it.
const (
	dependencyPath = "example.com/dependency"
	userPath       = "example.com/user"

	dependencySource = "package dependency\n\ntype Thing struct{ Size int }\n\nfunc (Thing) Measure() int { return 0 }\n"
	userSource       = "package user\n\nimport \"example.com/dependency\"\n\nvar Value dependency.Thing\n\n" +
		"func Make() dependency.Thing { return dependency.Thing{} }\n\ntype Wrapper struct{ dependency.Thing }\n"
)

// importerFunction is a function as a types.Importer.
type importerFunction func(path string) (*types.Package, error)

// Import is the function's answer for path.
func (importer importerFunction) Import(path string) (*types.Package, error) {
	return importer(path)
}

// writtenUser is user's export data, written from its source, and dependency as user's check had it.
func writtenUser(test *testing.T) ([]byte, *types.Package) {
	test.Helper()
	fileSet := token.NewFileSet()
	checked := func(path string, source string, importer types.Importer) *types.Package {
		parsed, parseError := parser.ParseFile(fileSet, path+".go", source, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", path, parseError)
		}
		typed, checkError := (&types.Config{Importer: importer}).Check(path, fileSet, []*ast.File{parsed}, nil)
		if checkError != nil {
			test.Fatalf("type-checking %s: %v", path, checkError)
		}
		return typed
	}
	dependency := checked(dependencyPath, dependencySource, nil)
	user := checked(userPath, userSource, importerFunction(func(path string) (*types.Package, error) {
		if path != dependencyPath {
			return nil, fmt.Errorf("no package %s", path)
		}
		return dependency, nil
	}))
	written, writeError := Write(fileSet, user)
	if writeError != nil {
		test.Fatalf("writing the export data of %s: %v", userPath, writeError)
	}
	return written, dependency
}

// Read against the packages user's export data reaches, the read's every object of dependency is the one closure
// holds, never a copy: one universe.
func TestAReadTakesEachPackageItNamesFromItsClosure(test *testing.T) {
	test.Parallel()

	written, dependency := writtenUser(test)
	closure := map[string]*types.Package{dependencyPath: dependency}
	read, readError := Read(token.NewFileSet(), closure, written, userPath)
	if readError != nil {
		test.Fatalf("reading the export data of %s: %v", userPath, readError)
	}
	if closure[userPath] != read {
		test.Errorf("want closure to hold the package read, got %v", closure[userPath])
	}
	valueType, isNamed := read.Scope().Lookup("Value").Type().(*types.Named)
	if !isNamed || valueType.Obj() != dependency.Scope().Lookup("Thing") {
		test.Errorf("want Value of dependency's own Thing, got %v", read.Scope().Lookup("Value").Type())
	}
}

// Read with dependency left out of closure is an error naming dependency's path, never a package the read makes in its
// place, as x/tools' GetPackagesFromMap would; and read against a dependency of the same path that does not declare
// what user names, it is an error too, and the dependency gains nothing. A closure that already holds a package of
// user's path is refused before anything is read.
func TestAReadRefusesAClosureWithoutWhatItNames(test *testing.T) {
	test.Parallel()

	written, _ := writtenUser(test)
	_, missingError := Read(token.NewFileSet(), map[string]*types.Package{}, written, userPath)
	if missingError == nil || !strings.Contains(missingError.Error(), "it names "+dependencyPath+", which is not "+
		"among the packages it is read against") {
		test.Errorf("want the read refused naming %s, got %v", dependencyPath, missingError)
	}

	empty := types.NewPackage(dependencyPath, "dependency")
	empty.MarkComplete()
	if _, emptyError := Read(token.NewFileSet(), map[string]*types.Package{dependencyPath: empty}, written,
		userPath); emptyError == nil {
		test.Error("want the read refused against a dependency that declares nothing, got a package")
	}
	if empty.Scope().Len() != 0 {
		test.Errorf("want the empty dependency left empty, got %v", empty.Scope().Names())
	}

	held := map[string]*types.Package{userPath: types.NewPackage(userPath, "user")}
	if _, heldError := Read(token.NewFileSet(), held, written, userPath); heldError == nil ||
		!strings.Contains(heldError.Error(), "hold one of its path already") {
		test.Errorf("want the read refused over a closure that holds %s, got %v", userPath, heldError)
	}
}
