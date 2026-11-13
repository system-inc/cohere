package load

import (
	"bytes"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cgo_testing"
)

// translationFiles are a project with cgo of its own: native, a cgo package with a plain file, an in-package test and
// an external test, and user, a cgo package too, which imports native and which native's external tests import, so
// go test builds it again as "user [native.test]". native imports os/user, which has cgo files on linux, and plugin,
// which has them on linux and darwin alike, so a standard-library package with cgo files is translated on either.
var translationFiles = map[string]string{
	"go.mod": "module example.com/translation\n\ngo 1.21\n",
	"native/native.go": "package native\n\n// static int twice(int value) { return value * 2; }\nimport \"C\"\n\n" +
		"import (\n\t_ \"os/user\"\n\t_ \"plugin\"\n)\n\n" +
		"// Twice is value doubled, by C.\nfunc Twice(value int) int { return int(C.twice(C.int(value))) }\n",
	"native/plain.go": "package native\n\n// One is one.\nfunc One() int { return 1 }\n",
	"native/native_test.go": "package native\n\nimport \"testing\"\n\n" +
		"func TestTwice(test *testing.T) { _ = Twice(One()) }\n",
	"native/external_test.go": "package native_test\n\nimport (\n\t\"testing\"\n\n" +
		"\t\"example.com/translation/native\"\n\t\"example.com/translation/user\"\n)\n\n" +
		"func TestUse(test *testing.T) { _ = native.Twice(user.Use()) }\n",
	"user/user.go": "package user\n\n// static int three(void) { return 3; }\nimport \"C\"\n\n" +
		"import \"example.com/translation/native\"\n\n" +
		"// Use is three doubled.\nfunc Use() int { return native.Twice(int(C.three())) }\n",
}

// translationEnvironment is what the translation fixture loads under: the host, whose C compiler the cgo tool runs,
// with cgo on.
var translationEnvironment = []string{"GOWORK=off", "CGO_ENABLED=1"}

// reachedByIdentifier is every package roots reach, by ID, except through go test's synthesized main, the one root
// that is no test variant and yet imports one: the load builds neither it nor what only it imports.
func reachedByIdentifier(roots []*packages.Package) map[string]*packages.Package {
	built := slices.DeleteFunc(slices.Clone(roots), func(root *packages.Package) bool {
		return root.ForTest == "" && slices.ContainsFunc(slices.Collect(maps.Values(root.Imports)),
			func(imported *packages.Package) bool { return imported.ForTest != "" })
	})
	reached := map[string]*packages.Package{}
	packages.Visit(built, nil, func(visited *packages.Package) { reached[visited.ID] = visited })
	return reached
}

// importIdentifiers is a package's imports, the ID each import path resolves to.
func importIdentifiers(loaded *packages.Package) map[string]string {
	identifiers := map[string]string{}
	for path, imported := range loaded.Imports {
		identifiers[path] = imported.ID
	}
	return identifiers
}

// typesImports is the paths of what a package's types import, in their order.
func typesImports(loaded *packages.Package) []string {
	var paths []string
	for _, imported := range loaded.Types.Imports() {
		paths = append(paths, imported.Path())
	}
	return paths
}

// scopeObjects is every object a package's scope declares, as types.ObjectString writes it.
func scopeObjects(loaded *packages.Package) []string {
	scope := loaded.Types.Scope()
	objects := make([]string, 0, scope.Len())
	for _, name := range scope.Names() {
		objects = append(objects, types.ObjectString(scope.Lookup(name), nil))
	}
	return objects
}

// translatedOutside reports whether a package's compiled files hold one outside its directory, the cgo tool's
// output in the build cache.
func translatedOutside(loaded *packages.Package) bool {
	return slices.ContainsFunc(loaded.CompiledGoFiles, func(file string) bool {
		return !strings.HasPrefix(file, loaded.Dir+string(filepath.Separator))
	})
}

// A project with cgo of its own loads through the load's own graph with every package with cgo files translated by go
// list -compiled, and each package of it is the one go/packages gives, with -compiled and -test, from the same
// environment: its ID, its files as written and as compiled, the cgo tool's outputs among them in go list's order,
// its other files, its errors, its imports, what its types import in their order, and every object its scope
// declares. The standard library's packages with cgo files are translated too, as go/packages translates them.
func TestACgoProjectLoadsAsGoPackagesLoadsIt(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)

	directory := test.TempDir()
	writeListingFiles(test, directory, translationFiles)
	loaded, loadError := Packages(directory, []string{"./..."}, translationEnvironment, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	if !loaded.CgoTranslated {
		test.Fatal("a project with cgo of its own was loaded with package \"C\" faked")
	}
	environment := translatedEnvironment(goEnvironmentOf(test, directory, translationEnvironment))
	reference, referenceError := packages.Load(&packages.Config{
		Mode:  packages.LoadAllSyntax | packages.NeedForTest | packages.NeedModule | packages.NeedEmbedFiles,
		Tests: true,
		Dir:   directory,
		Env:   environment,
	}, "./...")
	if referenceError != nil {
		test.Fatal(referenceError)
	}

	ours, theirs := reachedByIdentifier(loaded.Roots), reachedByIdentifier(reference)
	ourIdentifiers, theirIdentifiers := slices.Sorted(maps.Keys(ours)), slices.Sorted(maps.Keys(theirs))
	if !slices.Equal(ourIdentifiers, theirIdentifiers) {
		test.Fatalf("want the packages go/packages loaded\n%q\ngot\n%q", theirIdentifiers, ourIdentifiers)
	}
	translatedPaths := map[string]bool{}
	for _, identifier := range theirIdentifiers {
		our, their := ours[identifier], theirs[identifier]
		if translatedOutside(their) {
			translatedPaths[their.PkgPath] = true
		}
		for _, field := range []struct {
			name      string
			our       any
			their     any
			identical bool
		}{
			{"GoFiles", our.GoFiles, their.GoFiles, slices.Equal(our.GoFiles, their.GoFiles)},
			{"CompiledGoFiles", our.CompiledGoFiles, their.CompiledGoFiles,
				slices.Equal(our.CompiledGoFiles, their.CompiledGoFiles)},
			{"OtherFiles", our.OtherFiles, their.OtherFiles, slices.Equal(our.OtherFiles, their.OtherFiles)},
			{"Errors", our.Errors, their.Errors, slices.Equal(our.Errors, their.Errors)},
			{"imports", importIdentifiers(our), importIdentifiers(their),
				maps.Equal(importIdentifiers(our), importIdentifiers(their))},
			{"Types.Imports", typesImports(our), typesImports(their),
				slices.Equal(typesImports(our), typesImports(their))},
			{"scope", scopeObjects(our), scopeObjects(their), slices.Equal(scopeObjects(our), scopeObjects(their))},
		} {
			if !field.identical {
				test.Errorf("%s: want the %s go/packages gave\n%q\ngot\n%q", identifier, field.name, field.their,
					field.our)
			}
		}
	}
	for _, path := range []string{"example.com/translation/native", "example.com/translation/user", "plugin"} {
		if !translatedPaths[path] {
			test.Errorf("go/packages did not translate %s, so the fixture proves nothing of it", path)
		}
	}
	if loaded.CgoTranslatedPackages != len(translatedPaths) {
		test.Errorf("want the %d packages translated counted, got %d", len(translatedPaths),
			loaded.CgoTranslatedPackages)
	}
}

// A package the cgo tool translated fakes nothing, so the graph withholds nothing of it, though it is a dependency
// with cgo files: what withholding drives, its type errors judged and its declarations stripped, reads a faked
// package "C", and nothing in a translated package is. It is the one difference a load cannot show, since a
// translated package has nothing to withhold, so the graph is read directly.
func TestATranslatedDependencyWithholdsNothing(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)

	directory := test.TempDir()
	writeListingFiles(test, directory, translationFiles)
	goEnvironment := goEnvironmentOf(test, directory, translationEnvironment)
	listed, listError := list(goEnvironment, []string{"./..."}, nil, nil, nil, nil)
	if listError != nil {
		test.Fatal(listError)
	}
	compiled, compiledError := listCompiled(goEnvironment.guard, directory, translatedEnvironment(goEnvironment),
		listed)
	if compiledError != nil {
		test.Fatal(compiledError)
	}
	built := newGraph(listed, directory, compiled)
	plugin := built.nodes["plugin"]
	if plugin == nil || !plugin.translated {
		test.Fatalf("want plugin in the graph, translated, got %+v", plugin)
	}
	for identifier, each := range built.nodes {
		if each.translated && each.withholds {
			test.Errorf("%s was translated and yet withholds", identifier)
		}
	}
}

// heldExportData is a Restorer that holds the export data of every package whose path it names, from an earlier load,
// and records which loader it was told ran.
type heldExportData struct {
	held map[string]*Restored

	lock    sync.Mutex
	loaders []string
}

// Restore returns the held packages among reachable, by ID.
func (holder *heldExportData) Restore(
	loader string,
	reachable []*packages.Package,
	_ []*packages.Package,
) map[*packages.Package]*Restored {
	holder.lock.Lock()
	defer holder.lock.Unlock()
	holder.loaders = append(holder.loaders, loader)
	restored := map[*packages.Package]*Restored{}
	for _, each := range reachable {
		if held := holder.held[each.ID]; held != nil {
			restored[each] = held
		}
	}
	return restored
}

// A second load of a project with cgo of its own, with the findings cache holding its cgo package and that package's
// test variant, parses none of their files and reads both from export data, as on any other project, and the cache
// is told the load translated cgo, which its keys hold.
func TestACgoProjectsPackageIsRestoredFromExportData(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)

	directory := test.TempDir()
	writeListingFiles(test, directory, translationFiles)
	first, firstError := Packages(directory, []string{"./..."}, translationEnvironment, nil)
	if firstError != nil {
		test.Fatal(firstError)
	}
	holder := &heldExportData{held: map[string]*Restored{}}
	parsed := map[string]bool{}
	for identifier, each := range reachedByIdentifier(first.Roots) {
		if each.PkgPath != "example.com/translation/native" {
			continue
		}
		var exportData bytes.Buffer
		if writeError := gcexportdata.Write(&exportData, each.Fset, each.Types); writeError != nil {
			test.Fatalf("writing the export data of %s: %v", identifier, writeError)
		}
		holder.held[identifier] = &Restored{
			ReadExportData: func() ([]byte, error) { return exportData.Bytes(), nil },
			Imports:        typesImports(each),
		}
		for _, file := range each.CompiledGoFiles {
			parsed[file] = true
		}
	}
	if len(holder.held) != 2 {
		test.Fatalf("want native and its test variant held, got %q", slices.Sorted(maps.Keys(holder.held)))
	}

	second, secondError := PackagesCached(directory, []string{"./..."}, translationEnvironment, nil,
		Caching{Restorer: holder})
	if secondError != nil {
		test.Fatal(secondError)
	}
	if len(second.Diagnostics) > 0 {
		test.Fatalf("the restored load has diagnostics: %+v", second.Diagnostics)
	}
	if !slices.Equal(holder.loaders, []string{LoaderCgoTranslated}) {
		test.Errorf("want the cache told the load translated cgo once, got %q", holder.loaders)
	}
	if second.PackagesFromExportData != 2 {
		test.Errorf("want native and its test variant read from export data, got %d packages",
			second.PackagesFromExportData)
	}
	if dropped := first.FilesParsed - second.FilesParsed; dropped != len(parsed) {
		test.Errorf("want the %d files of native and its test variant left unparsed, got %d parsed of %d, %d fewer",
			len(parsed), second.FilesParsed, first.FilesParsed, dropped)
	}
}

// A cgo file that includes a header that does not exist fails the cgo tool, and the failure is a list diagnostic,
// with the compiler's words, never a package checked with nothing said; and the package is not counted translated.
func TestACgoTranslationThatFailsIsADiagnostic(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)

	directory := test.TempDir()
	writeListingFiles(test, directory, map[string]string{
		"go.mod": "module example.com/failing\n\ngo 1.21\n",
		"broken/broken.go": "package broken\n\n// #include \"missing.h\"\nimport \"C\"\n\n" +
			"// Use calls what the missing header would declare.\nfunc Use() { C.nothing() }\n",
		"broken/plain.go": "package broken\n\n// One is one.\nfunc One() int { return 1 }\n",
	})
	loaded, loadError := Packages(directory, []string{"./..."}, translationEnvironment, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	var found []Diagnostic
	for _, diagnostic := range loaded.Diagnostics {
		if diagnostic.Kind == KindList && strings.Contains(diagnostic.Message, "missing.h") {
			found = append(found, diagnostic)
		}
	}
	if len(found) != 1 {
		test.Fatalf("want one list diagnostic naming missing.h, got %+v", loaded.Diagnostics)
	}
	if loaded.CgoTranslatedPackages != 1 {
		test.Errorf("want runtime/cgo alone counted translated, got %d packages", loaded.CgoTranslatedPackages)
	}
}
