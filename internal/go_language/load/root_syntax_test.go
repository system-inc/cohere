package load

import (
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"
)

// rootSyntaxFiles are a module whose package root imports dependency, a package of the same module the pattern does
// not name, so it is no root. dependency has a type error, which a load keeps whatever it keeps of its syntax, and a
// package-level variable with an initial value, which the checker records in InitOrder.
var rootSyntaxFiles = map[string]string{
	"go.mod": "module example.com/rootsyntax\n\ngo 1.21\n",
	"root/root.go": "package root\n\nimport \"example.com/rootsyntax/dependency\"\n\n" +
		"// Twice is twice the dependency's value.\nfunc Twice() int { return dependency.Value() * 2 }\n",
	"dependency/dependency.go": "package dependency\n\nvar start = 1 + 2\n\n" +
		"// Value is the start.\nfunc Value() int { return start }\n\n// Broken is of the wrong type.\n" +
		"var Broken int = \"text\"\n",
}

// rootSyntaxLoads are rootSyntaxFiles written under a directory of the test's and loaded twice, with the roots' syntax
// alone and as PackagesWithOverlay loads it, each with the package root and the package dependency.
func rootSyntaxLoads(test *testing.T) (lean *Loaded, whole *Loaded, packagesOf func(*Loaded) (root,
	dependency *packages.Package)) {
	test.Helper()

	directory := test.TempDir()
	for name, contents := range rootSyntaxFiles {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if directoryError := os.MkdirAll(filepath.Dir(path), 0o755); directoryError != nil {
			test.Fatal(directoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	var loadError error
	lean, loadError = PackagesWithRootSyntax(directory, []string{"./root"}, []string{"GOWORK=off"}, nil, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	whole, loadError = PackagesWithOverlay(directory, []string{"./root"}, []string{"GOWORK=off"}, nil, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	packagesOf = func(loaded *Loaded) (*packages.Package, *packages.Package) {
		if len(loaded.Roots) != 1 || loaded.Roots[0].PkgPath != "example.com/rootsyntax/root" {
			test.Fatalf("want the one root example.com/rootsyntax/root, got %d roots", len(loaded.Roots))
		}
		dependency := loaded.Roots[0].Imports["example.com/rootsyntax/dependency"]
		if dependency == nil {
			test.Fatal("the root imports no example.com/rootsyntax/dependency")
		}
		return loaded.Roots[0], dependency
	}
	return lean, whole, packagesOf
}

// typeMapSizes is how many entries each map and InitOrder of info hold, by name, so a test names the one that is not
// as it wants.
func typeMapSizes(info *types.Info) map[string]int {
	return map[string]int{
		"Types":        len(info.Types),
		"Defs":         len(info.Defs),
		"Uses":         len(info.Uses),
		"Implicits":    len(info.Implicits),
		"Instances":    len(info.Instances),
		"Scopes":       len(info.Scopes),
		"Selections":   len(info.Selections),
		"FileVersions": len(info.FileVersions),
		"InitOrder":    len(info.InitOrder),
	}
}

// A load with the roots' syntax alone leaves a dependency without its syntax or type maps and with its types whole,
// keeps a root's syntax and maps, and reports the dependency's errors as the whole load does.
func TestADependencyOfARootSyntaxLoadKeepsItsTypesAlone(test *testing.T) {
	test.Parallel()

	lean, whole, packagesOf := rootSyntaxLoads(test)
	root, dependency := packagesOf(lean)
	if len(dependency.Syntax) != 0 {
		test.Errorf("the dependency kept %d files of syntax", len(dependency.Syntax))
	}
	if dependency.TypesInfo == nil {
		test.Fatal("the dependency has no TypesInfo, where a reader that checks for one wants an empty one")
	}
	for name, size := range typeMapSizes(dependency.TypesInfo) {
		if size != 0 {
			test.Errorf("the dependency's TypesInfo.%s holds %d entries, want none", name, size)
		}
	}
	if dependency.Types == nil || !dependency.Types.Complete() || dependency.Types.Scope().Lookup("Value") == nil ||
		dependency.Types.Scope().Lookup("Broken") == nil {
		test.Fatalf("the dependency's types are not whole: %v", dependency.Types)
	}
	if dependency.Fset == nil || len(dependency.Errors) != 1 || !dependency.IllTyped {
		test.Fatalf("want the dependency's file set and its one error, got %+v", dependency.Errors)
	}
	if !slices.Equal(lean.Diagnostics, whole.Diagnostics) || len(lean.Diagnostics) != 1 {
		test.Fatalf("want the whole load's one diagnostic, got %+v, want %+v", lean.Diagnostics, whole.Diagnostics)
	}
	if len(root.Syntax) != 1 {
		test.Errorf("the root kept %d files of syntax, want 1", len(root.Syntax))
	}
	rootSizes := typeMapSizes(root.TypesInfo)
	for _, name := range []string{"Types", "Defs", "Uses", "Scopes", "FileVersions"} {
		if rootSizes[name] == 0 {
			test.Errorf("the root's TypesInfo.%s is empty", name)
		}
	}
	if !slices.Equal(lean.Packages, lean.Roots) {
		test.Errorf("want the root analyzed, got %d packages", len(lean.Packages))
	}
}

// Without the option a dependency keeps its syntax and every map it filled, as go/packages gave it.
func TestADependencyOfAWholeLoadKeepsItsSyntaxAndMaps(test *testing.T) {
	test.Parallel()

	_, whole, packagesOf := rootSyntaxLoads(test)
	_, dependency := packagesOf(whole)
	if len(dependency.Syntax) != 1 {
		test.Errorf("the dependency kept %d files of syntax, want 1", len(dependency.Syntax))
	}
	sizes := typeMapSizes(dependency.TypesInfo)
	for _, name := range []string{"Types", "Defs", "Uses", "Scopes", "FileVersions", "InitOrder"} {
		if sizes[name] == 0 {
			test.Errorf("the dependency's TypesInfo.%s is empty", name)
		}
	}
}

// A dependency that withholds what reaches its cgo files judges its errors and withholds its declarations by its syntax
// and type maps before it lets them go, so a load with the roots' syntax alone withholds exactly what the whole load
// does, and still leaves it without them.
func TestARootSyntaxLoadWithholdsWhatTheWholeLoadDoes(test *testing.T) {
	test.Parallel()

	root := loadedCgoFixtures(test)
	lean, loadError := loadWorkspace(filepath.Join(root, "project"), []string{"./..."}, cgoEnvironment, nil, false,
		Caching{rootSyntaxOnly: true}, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	whole := cgoFixtures.project
	if !slices.Equal(lean.WithheldInDependencyCgo, whole.WithheldInDependencyCgo) ||
		len(lean.WithheldInDependencyCgo) == 0 {
		test.Errorf("want withheld %+v, got %+v", whole.WithheldInDependencyCgo, lean.WithheldInDependencyCgo)
	}
	if lean.WithheldDependencyCgoDeclarations != whole.WithheldDependencyCgoDeclarations ||
		lean.WithheldDependencyCgoDeclarations == 0 {
		test.Errorf("want %d declarations withheld, got %d", whole.WithheldDependencyCgoDeclarations,
			lean.WithheldDependencyCgoDeclarations)
	}
	if !slices.Equal(lean.Diagnostics, whole.Diagnostics) {
		test.Errorf("want the whole load's diagnostics %+v, got %+v", whole.Diagnostics, lean.Diagnostics)
	}
	withheldByIdentifier := func(loaded *Loaded) map[string]PackageWithheld {
		byIdentifier := map[string]PackageWithheld{}
		for withheldPackage, owned := range loaded.Withheld {
			byIdentifier[withheldPackage.ID] = owned
		}
		return byIdentifier
	}
	if !maps.EqualFunc(withheldByIdentifier(lean), withheldByIdentifier(whole), func(left, right PackageWithheld) bool {
		return slices.Equal(left.Errors, right.Errors) && slices.Equal(left.Declarations, right.Declarations)
	}) {
		test.Errorf("want withheld by package %+v, got %+v", withheldByIdentifier(whole), withheldByIdentifier(lean))
	}
	packages.Visit(lean.Roots, nil, func(visited *packages.Package) {
		if visited.ID != "example.com/dependency" {
			return
		}
		if visited.TypesInfo == nil {
			test.Fatal("the withholding dependency has no TypesInfo")
		}
		if len(visited.Syntax) != 0 || len(visited.TypesInfo.Uses)+len(visited.TypesInfo.Types) != 0 {
			test.Errorf("the withholding dependency kept %d files of syntax and %d uses", len(visited.Syntax),
				len(visited.TypesInfo.Uses))
		}
	})
}
