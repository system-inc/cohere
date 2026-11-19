package cache

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
)

// moduleFiles are a third-party module's files below a module cache: example.com/dependency at v1.0.0, whose root
// package imports fmt and whose inner package imports the root, as go lays a module out below `GOMODCACHE`.
var moduleFiles = map[string]string{
	"example.com/dependency@v1.0.0/dependency.go": "package dependency\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n",
	"example.com/dependency@v1.0.0/inner/inner.go": "package inner\n\nimport \"example.com/dependency\"\n\n" +
		"var _ = dependency.Value\n",
}

// Each package of moduleFiles by ID, the ID its import path, as go list names a dependency's package.
const (
	dependencyId = "example.com/dependency"
	innerId      = "example.com/dependency/inner"
)

// everyModuleId is every package of moduleFiles.
var everyModuleId = []string{dependencyId, innerId}

// writeModuleTree writes moduleFiles below moduleCache.
func writeModuleTree(test *testing.T, moduleCache string) {
	test.Helper()

	for relative, contents := range moduleFiles {
		path := filepath.Join(moduleCache, filepath.FromSlash(relative))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
}

// newModuleTree writes a standard library and a module below roots of the test's own, and returns the context keying
// both, its ModuleCache set, and the graph: standardGraph's packages, and the module's, which import fmt, as the load
// gives them, each with its module, not a main one, and files absolute.
func newModuleTree(test *testing.T) (StandardLibraryContext, map[string]*packages.Package) {
	test.Helper()

	goroot, graph := newStandardTree(test)
	moduleCache := test.TempDir()
	writeModuleTree(test, moduleCache)
	module := &packages.Module{Path: dependencyId, Version: "v1.0.0", GoVersion: "1.21"}
	directory := filepath.Join(moduleCache, "example.com", "dependency@v1.0.0")
	dependency := &packages.Package{
		ID: dependencyId, PkgPath: dependencyId, Name: "dependency", Module: module,
		GoFiles:         []string{filepath.Join(directory, "dependency.go")},
		CompiledGoFiles: []string{filepath.Join(directory, "dependency.go")},
		Imports:         map[string]*packages.Package{"fmt": graph[fmtId]},
	}
	graph[dependencyId] = dependency
	graph[innerId] = &packages.Package{
		ID: innerId, PkgPath: innerId, Name: "inner", Module: module,
		GoFiles:         []string{filepath.Join(directory, "inner", "inner.go")},
		CompiledGoFiles: []string{filepath.Join(directory, "inner", "inner.go")},
		Imports:         map[string]*packages.Package{dependencyId: dependency},
	}
	shared := standardContextAt(goroot)
	shared.ModuleCache = moduleCache
	return shared, graph
}

// A module's package that is not a main module's, every file below the module cache, is kept by the system cache
// beside the standard library: each file is named in its key below the module cache, `$GOMODCACHE/` then its path, a
// marker apart from `$GOROOT/`, every package of the tree is keyed, and every key is its own.
func TestAModulesPackageBelowTheModuleCacheIsKeyedWithItsMarker(test *testing.T) {
	test.Parallel()

	shared, graph := newModuleTree(test)
	keys := keyStandard(test, graph, shared)
	expectChanged(test, "keying the tree", map[string]Key{}, keys, append(everyModuleId, everyStandardId...)...)
	distinct := map[Key]string{}
	for id, key := range keys {
		if other, taken := distinct[key]; taken {
			test.Errorf("%s and %s share the key %s", id, other, key)
		}
		distinct[key] = id
	}

	keyer := &standardLibraryKeyer{goroot: shared.Goroot, moduleCache: shared.ModuleCache,
		rootRelative: map[string]string{}, system: map[*packages.Package]bool{}, visiting: map[*packages.Package]bool{}}
	if system, judgeError := keyer.isSystem(graph[innerId]); judgeError != nil || !system {
		test.Fatalf("want %s judged the system cache's, got %t, %v", innerId, system, judgeError)
	}
	for file, want := range map[string]string{
		graph[dependencyId].GoFiles[0]: "$GOMODCACHE/example.com/dependency@v1.0.0/dependency.go",
		graph[innerId].GoFiles[0]:      "$GOMODCACHE/example.com/dependency@v1.0.0/inner/inner.go",
		graph[fmtId].GoFiles[0]:        "$GOROOT/src/fmt/print.go",
	} {
		if named := keyer.rootRelative[file]; named != want {
			test.Errorf("%s is named %q in its key, want %q", file, named, want)
		}
	}
}

// A package the system cache does not keep gets no key, and neither does any package that imports it, while the
// standard library keeps every key: a main module's package below the module cache, as under go.work, where every
// workspace module is a main module, or a project checked out below the module cache; a module's package with one
// file outside the module cache, as a vendored module's or one a replace points at a directory of the project's;
// a module's package that imports a project package; and every module's package when the context names no module
// cache.
func TestAPackageTheSystemCacheDoesNotKeepGetsNoKey(test *testing.T) {
	test.Parallel()

	for name, outside := range map[string]struct {
		change  func(test *testing.T, shared *StandardLibraryContext, graph map[string]*packages.Package)
		without []string
	}{
		"a main module's package": {func(_ *testing.T, _ *StandardLibraryContext, graph map[string]*packages.Package) {
			mainModule := *graph[dependencyId].Module
			mainModule.Main = true
			graph[dependencyId].Module = &mainModule
		}, everyModuleId},
		"a vendored module's package": {func(test *testing.T, _ *StandardLibraryContext,
			graph map[string]*packages.Package) {
			vendored := filepath.Join(test.TempDir(), "vendor", "example.com", "dependency", "dependency.go")
			if makeError := os.MkdirAll(filepath.Dir(vendored), 0o755); makeError != nil {
				test.Fatal(makeError)
			}
			if writeError := os.WriteFile(vendored, []byte(moduleFiles["example.com/dependency@v1.0.0/dependency.go"]),
				0o644); writeError != nil {
				test.Fatal(writeError)
			}
			graph[dependencyId].GoFiles = []string{vendored}
			graph[dependencyId].CompiledGoFiles = []string{vendored}
		}, everyModuleId},
		"a module's package with one file a replace points outside": {func(test *testing.T, _ *StandardLibraryContext,
			graph map[string]*packages.Package) {
			replaced := filepath.Join(test.TempDir(), "extra.go")
			if writeError := os.WriteFile(replaced, []byte("package inner\n"), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			graph[innerId].GoFiles = append(graph[innerId].GoFiles, replaced)
		}, []string{innerId}},
		"a module's package importing a project package": {func(test *testing.T, _ *StandardLibraryContext,
			graph map[string]*packages.Package) {
			// The project's file is outside the module cache, so it is the project's whatever its module says.
			projectFile := filepath.Join(test.TempDir(), "project.go")
			if writeError := os.WriteFile(projectFile, []byte("package project\n"), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			graph["example.com/project"] = &packages.Package{
				ID: "example.com/project", PkgPath: "example.com/project", Name: "project",
				Module:  &packages.Module{Path: "example.com/project", Main: true},
				GoFiles: []string{projectFile},
			}
			graph[dependencyId].Imports["example.com/project"] = graph["example.com/project"]
		}, everyModuleId},
		"no module cache": {func(_ *testing.T, shared *StandardLibraryContext, _ map[string]*packages.Package) {
			shared.ModuleCache = ""
		}, everyModuleId},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			shared, graph := newModuleTree(test)
			keys := keyStandard(test, graph, shared)
			outside.change(test, &shared, graph)
			after := keyStandard(test, graph, shared)
			expectChanged(test, name, keys, after, outside.without...)
			for _, id := range append(outside.without, "example.com/project") {
				if _, keyed := after[id]; keyed {
					test.Errorf("%s has a key, want none", id)
				}
			}
		})
	}
}

// The module cache moved, the same files below another directory, keys every module's package apart, since its entry
// holds this machine's paths, and no standard-library package: nothing below `GOROOT` reads a module. A module cache
// named relative to nothing is refused, as a relative `GOROOT` is.
func TestAModuleCacheMovedKeysEveryModulesPackageApart(test *testing.T) {
	test.Parallel()

	shared, graph := newModuleTree(test)
	keys := keyStandard(test, graph, shared)
	elsewhere := test.TempDir()
	writeModuleTree(test, elsewhere)
	moved := shared
	moved.ModuleCache = elsewhere
	for _, id := range everyModuleId {
		for index, file := range graph[id].GoFiles {
			relative, relativeError := filepath.Rel(shared.ModuleCache, file)
			if relativeError != nil {
				test.Fatal(relativeError)
			}
			graph[id].GoFiles[index] = filepath.Join(elsewhere, relative)
		}
		graph[id].CompiledGoFiles = graph[id].GoFiles
	}
	expectChanged(test, "the same module below another module cache", keys, keyStandard(test, graph, moved),
		everyModuleId...)

	relative := shared
	relative.ModuleCache = filepath.Join("go", "pkg", "mod")
	if refused, keysError := StandardLibraryKeys([]*packages.Package{graph[innerId]}, relative,
		FileDigests{}); keysError == nil {
		test.Errorf("a relative module cache: want an error, got %d keys", len(refused))
	}
}

// What a module's entry holds beside its files keys it apart, and the standard library with it where the change is
// the context's: another option of a fact analyzer, so two projects with different settings never share an analysis,
// another go version of its module, the language version its files are checked under, and a byte more in its file,
// which keys its importer apart too.
func TestWhatAModulesEntryHoldsKeysItApart(test *testing.T) {
	test.Parallel()

	for name, change := range map[string]struct {
		apply func(test *testing.T, shared *StandardLibraryContext, graph map[string]*packages.Package)
		want  []string
	}{
		"a fact analyzer's option": {func(_ *testing.T, shared *StandardLibraryContext,
			_ map[string]*packages.Package) {
			shared.Facts[0] = `fact_purity {"strict":"true"}`
		}, append(append([]string{}, everyModuleId...), everyStandardId...)},
		"the module's go version": {func(_ *testing.T, _ *StandardLibraryContext, graph map[string]*packages.Package) {
			newer := *graph[dependencyId].Module
			newer.GoVersion = "1.22"
			graph[dependencyId].Module, graph[innerId].Module = &newer, &newer
		}, everyModuleId},
		"a byte of the root package's file": {func(test *testing.T, _ *StandardLibraryContext,
			graph map[string]*packages.Package) {
			file := graph[dependencyId].GoFiles[0]
			contents, readError := os.ReadFile(file)
			if readError != nil {
				test.Fatal(readError)
			}
			if writeError := os.WriteFile(file, append(contents, '\n'), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
		}, everyModuleId},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			shared, graph := newModuleTree(test)
			keys := keyStandard(test, graph, shared)
			change.apply(test, &shared, graph)
			expectChanged(test, name, keys, keyStandard(test, graph, shared), change.want...)
		})
	}
}
