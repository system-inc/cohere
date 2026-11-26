package intermediate_representation_test

// Building one function builds only it (#6pt3bt8, wave 3b).

import (
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
)

// TestBuildingOneFunctionBuildsOnlyIt builds strings.Title, which holds a function literal, from a package nothing
// else of has been built, and holds every other function of the package unbuilt. A Build that built the whole
// package fails it.
func TestBuildingOneFunctionBuildsOnlyIt(test *testing.T) {
	test.Parallel()
	loaded, loadError := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedFiles |
		packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
		packages.NeedImports}, "strings")
	if loadError != nil || len(loaded) != 1 || len(loaded[0].Errors) > 0 {
		test.Fatalf("loading strings: %v %v", loadError, loaded)
	}
	unit := differentialUnit{path: loaded[0].PkgPath, fileSet: loaded[0].Fset, types: loaded[0].Types,
		files: loaded[0].Syntax, typeInfo: loaded[0].TypesInfo}
	program := intermediate_representation.NewProgram(unit.fileSet)
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	title := built.Function("Title")
	title.Build()

	if title.Blocks == nil {
		test.Fatal("strings.Title has no blocks after Build")
	}
	if len(title.AnonymousFunctions) == 0 {
		test.Fatal("strings.Title holds no anonymous function, so the test doesn't hold one to being built with it")
	}
	for _, anonymous := range title.AnonymousFunctions {
		if anonymous.Blocks == nil {
			test.Errorf("%s, declared in strings.Title, wasn't built with it", anonymous.RelativeString(nil))
		}
	}
	others := 0
	for _, function := range built.Functions {
		if function == title {
			continue
		}
		others++
		if function.Blocks != nil {
			test.Errorf("%s was built, though only strings.Title was asked for", function.RelativeString(nil))
		}
	}
	if others < 50 {
		test.Errorf("strings holds %d other functions, too few for the test to mean much", others)
	}
}

// differentialCreateOurs creates unit's package and its imports' in a program of their own, building nothing.
func differentialCreateOurs(unit differentialUnit) *intermediate_representation.Package {
	program := intermediate_representation.NewProgram(unit.fileSet)
	program.SetNoReturn(differentialNoReturn)
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	return program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
}
