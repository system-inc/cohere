package cache

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/build_inputs"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// standardFiles are a small Go installation's files below its root: errors, with a file the constraints leave out;
// unsafe; runtime/cgo; fmt, importing errors; net, with a cgo file, importing errors and what cgo adds; and net/http,
// importing fmt and net, which has no cgo files of its own and reaches one.
var standardFiles = map[string]string{
	"src/errors/errors.go":         "package errors\n",
	"src/errors/errors_windows.go": "package errors\n\nconst windows = true\n",
	"src/unsafe/unsafe.go":         "package unsafe\n",
	"src/runtime/cgo/cgo.go":       "package cgo\n\nimport \"C\"\n",
	"src/fmt/print.go":             "package fmt\n\nimport \"errors\"\n\nvar _ = errors.New\n",
	"src/net/net.go":               "package net\n\nimport \"errors\"\n\nvar _ = errors.New\n",
	"src/net/cgo_unix.go":          "package net\n\n// #include <netdb.h>\nimport \"C\"\n",
	"src/net/http/server.go":       "package http\n\nimport (\n\t\"fmt\"\n\t\"net\"\n)\n",
}

// Each package of standardFiles by ID, the ID its import path, as go list names a standard-library package.
const (
	errorsId     = "errors"
	unsafeId     = "unsafe"
	runtimeCgoId = "runtime/cgo"
	fmtId        = "fmt"
	netId        = "net"
	httpId       = "net/http"
)

// everyStandardId is every package of standardFiles.
var everyStandardId = []string{errorsId, unsafeId, runtimeCgoId, fmtId, netId, httpId}

// standardContext is the context every test keys under unless it is the context being changed, its Goroot set to
// the tree's.
var standardContext = StandardLibraryContext{
	FormatVersion: "1",
	ReleaseStamp:  "v0.2.0 4e1427954",
	Build: []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1", "GOFLAGS=-tags=probe", "GOVERSION=go1.27.3",
		"GOEXPERIMENT="},
	Facts:  []string{`fact_purity {"strict":"false"}`, "nilness"},
	Loader: load.LoaderSource,
}

// standardContextAt is standardContext with goroot, its lists its own, so a test can change them.
func standardContextAt(goroot string) StandardLibraryContext {
	shared := standardContext
	shared.Build = slices.Clone(standardContext.Build)
	shared.Facts = slices.Clone(standardContext.Facts)
	shared.Goroot = goroot
	return shared
}

// writeStandardTree writes standardFiles below goroot.
func writeStandardTree(test *testing.T, goroot string) {
	test.Helper()

	for relative, contents := range standardFiles {
		path := filepath.Join(goroot, filepath.FromSlash(relative))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
}

// standardGraph is standardFiles below goroot as the load gives its packages: no module, files absolute, each list
// its own slice, and a package with cgo files importing what the cgo tool adds.
func standardGraph(goroot string) map[string]*packages.Package {
	files := func(relatives ...string) []string {
		absolute := make([]string, 0, len(relatives))
		for _, relative := range relatives {
			absolute = append(absolute, filepath.Join(goroot, filepath.FromSlash(relative)))
		}
		return absolute
	}
	unsafePackage := &packages.Package{
		ID: unsafeId, PkgPath: unsafeId, Name: "unsafe",
		GoFiles: files("src/unsafe/unsafe.go"),
	}
	errorsPackage := &packages.Package{
		ID: errorsId, PkgPath: errorsId, Name: "errors",
		GoFiles:         files("src/errors/errors.go"),
		CompiledGoFiles: files("src/errors/errors.go"),
		IgnoredFiles:    files("src/errors/errors_windows.go"),
		Imports:         map[string]*packages.Package{},
	}
	runtimeCgo := &packages.Package{
		ID: runtimeCgoId, PkgPath: runtimeCgoId, Name: "cgo",
		GoFiles:         files("src/runtime/cgo/cgo.go"),
		CompiledGoFiles: files("src/runtime/cgo/cgo.go"),
		Imports:         map[string]*packages.Package{"unsafe": unsafePackage},
	}
	fmtPackage := &packages.Package{
		ID: fmtId, PkgPath: fmtId, Name: "fmt",
		GoFiles:         files("src/fmt/print.go"),
		CompiledGoFiles: files("src/fmt/print.go"),
		Imports:         map[string]*packages.Package{"errors": errorsPackage},
	}
	netPackage := &packages.Package{
		ID: netId, PkgPath: netId, Name: "net",
		GoFiles:         files("src/net/net.go", "src/net/cgo_unix.go"),
		CompiledGoFiles: files("src/net/net.go", "src/net/cgo_unix.go"),
		Imports: map[string]*packages.Package{"errors": errorsPackage, "runtime/cgo": runtimeCgo,
			"unsafe": unsafePackage},
	}
	httpPackage := &packages.Package{
		ID: httpId, PkgPath: httpId, Name: "http",
		GoFiles:         files("src/net/http/server.go"),
		CompiledGoFiles: files("src/net/http/server.go"),
		Imports:         map[string]*packages.Package{"fmt": fmtPackage, "net": netPackage},
	}
	return map[string]*packages.Package{
		errorsId: errorsPackage, unsafeId: unsafePackage, runtimeCgoId: runtimeCgo, fmtId: fmtPackage,
		netId: netPackage, httpId: httpPackage,
	}
}

// newStandardTree writes standardFiles below a root of the test's own and returns the root and the graph.
func newStandardTree(test *testing.T) (string, map[string]*packages.Package) {
	test.Helper()

	goroot := test.TempDir()
	writeStandardTree(test, goroot)
	return goroot, standardGraph(goroot)
}

// keyStandard keys every package of graph under shared, given in the order of their IDs, and returns the keys by ID.
func keyStandard(test *testing.T, graph map[string]*packages.Package, shared StandardLibraryContext) map[string]Key {
	test.Helper()

	reachable := make([]*packages.Package, 0, len(graph))
	for _, id := range slices.Sorted(maps.Keys(graph)) {
		reachable = append(reachable, graph[id])
	}
	keys, keysError := StandardLibraryKeys(reachable, shared, FileDigests{})
	if keysError != nil {
		test.Fatalf("keying the standard library: %v", keysError)
	}
	return keysById(keys)
}

// Every package of the tree is keyed, each differently, and keying it twice gives the same keys.
func TestStandardLibraryKeysCoverEveryStandardPackageAndAreTheSameAcrossCalls(test *testing.T) {
	test.Parallel()

	goroot, graph := newStandardTree(test)
	keys := keyStandard(test, graph, standardContextAt(goroot))
	expectChanged(test, "keying the tree", map[string]Key{}, keys, everyStandardId...)
	distinct := map[Key]string{}
	for id, key := range keys {
		if other, taken := distinct[key]; taken {
			test.Errorf("%s and %s share the key %s", id, other, key)
		}
		distinct[key] = id
	}
	expectChanged(test, "keying twice", keys, keyStandard(test, graph, standardContextAt(goroot)))
}

// relocatedKeys keys the same tree written below another root, so a key that holds where the installation is
// fails the comparison with the tree's keys.
func relocatedKeys(test *testing.T, keys map[string]Key) {
	test.Helper()

	elsewhere, elsewhereGraph := newStandardTree(test)
	moved := keyStandard(test, elsewhereGraph, standardContextAt(elsewhere))
	expectChanged(test, "the same files below another root", keys, moved)
}

// Each field of the context changes what it must, and only that: the format version, the release stamp, each
// entry of the build and a fact analyzer's flag change every key; the loader changes the key of a package with cgo
// files and of every package that imports one, and of no other; and the root changes none when the files below it
// are the same. A new field without a case here fails, since the fields are found by reflection.
func TestEachStandardLibraryContextFieldChangesWhatItMust(test *testing.T) {
	test.Parallel()

	goroot, graph := newStandardTree(test)
	keys := keyStandard(test, graph, standardContextAt(goroot))
	changeEvery := func(change func(*StandardLibraryContext)) func(*testing.T) {
		return func(test *testing.T) {
			changed := standardContextAt(goroot)
			change(&changed)
			expectChanged(test, "the changed context", keys, keyStandard(test, graph, changed), everyStandardId...)
		}
	}
	cases := map[string]func(*testing.T){
		"FormatVersion": changeEvery(func(changed *StandardLibraryContext) { changed.FormatVersion += "+" }),
		"ReleaseStamp":  changeEvery(func(changed *StandardLibraryContext) { changed.ReleaseStamp += "+" }),
		"Build": func(test *testing.T) {
			for index, entry := range standardContext.Build {
				name, _, _ := strings.Cut(entry, "=")
				test.Run(name, func(test *testing.T) {
					test.Parallel()

					changeEvery(func(changed *StandardLibraryContext) { changed.Build[index] = entry + "+" })(test)
				})
			}
		},
		"Facts": changeEvery(func(changed *StandardLibraryContext) {
			changed.Facts[0] = `fact_purity {"strict":"true"}`
		}),
		"Loader": func(test *testing.T) {
			changed := standardContextAt(goroot)
			changed.Loader = load.LoaderCgoTranslated
			expectChanged(test, "the loader", keys, keyStandard(test, graph, changed), runtimeCgoId, netId, httpId)
		},
		"Goroot": func(test *testing.T) {
			relocatedKeys(test, keys)
		},
	}
	fields := reflect.TypeFor[StandardLibraryContext]()
	for index := range fields.NumField() {
		name := fields.Field(index).Name
		check, known := cases[name]
		if !known {
			test.Errorf("StandardLibraryContext.%s has no case here: say what changing it does to the keys", name)
			continue
		}
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			check(test)
		})
	}
}

// The same standard library below two different roots keys alike, each copy's files read from its own root: a key
// holds where a file is in the installation, never where the installation is, so a release's keys are a user's.
func TestTheSameStandardLibraryBelowAnotherRootKeysAlike(test *testing.T) {
	test.Parallel()

	goroot, graph := newStandardTree(test)
	keys := keyStandard(test, graph, standardContextAt(goroot))
	relocatedKeys(test, keys)
}

// A byte more in a file changes the key of its package and of every package that imports it, and of no other.
func TestAByteOfAStandardFileChangesItsPackageAndItsImporters(test *testing.T) {
	test.Parallel()

	goroot, graph := newStandardTree(test)
	keys := keyStandard(test, graph, standardContextAt(goroot))
	file := filepath.Join(goroot, "src", "errors", "errors.go")
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatal(readError)
	}
	if writeError := os.WriteFile(file, append(contents, '\n'), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	expectChanged(test, "a byte of errors.go", keys, keyStandard(test, graph, standardContextAt(goroot)), errorsId,
		fmtId, netId, httpId)
}

// A file moved to another path below the root, its bytes the same, changes the key of its package and its
// importers: the path below the root is in the key.
func TestAStandardFilesPathBelowTheRootChangesItsPackageAndItsImporters(test *testing.T) {
	test.Parallel()

	goroot, graph := newStandardTree(test)
	keys := keyStandard(test, graph, standardContextAt(goroot))
	from := filepath.Join(goroot, "src", "errors", "errors.go")
	to := filepath.Join(goroot, "src", "errors", "wrap.go")
	if renameError := os.Rename(from, to); renameError != nil {
		test.Fatal(renameError)
	}
	graph[errorsId].GoFiles = []string{to}
	graph[errorsId].CompiledGoFiles = []string{to}
	expectChanged(test, "errors.go moved to wrap.go", keys, keyStandard(test, graph, standardContextAt(goroot)),
		errorsId, fmtId, netId, httpId)
}

// An import path added to a package's import map, or one renamed, changes its key and its importers', even when the
// path reaches a package already imported, so no dependency's key changes.
func TestAStandardImportChangesItsPackageAndItsImporters(test *testing.T) {
	test.Parallel()

	for name, change := range map[string]func(*packages.Package){
		"added": func(fmtPackage *packages.Package) {
			fmtPackage.Imports["errors/alias"] = fmtPackage.Imports["errors"]
		},
		"renamed": func(fmtPackage *packages.Package) {
			fmtPackage.Imports["errors/alias"] = fmtPackage.Imports["errors"]
			delete(fmtPackage.Imports, "errors")
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			goroot, graph := newStandardTree(test)
			keys := keyStandard(test, graph, standardContextAt(goroot))
			change(graph[fmtId])
			expectChanged(test, "an import path of fmt "+name, keys,
				keyStandard(test, graph, standardContextAt(goroot)), fmtId, httpId)
		})
	}
}

// A package with a module, or with a file that is not below the root, is not the standard library's, and neither
// is any package that imports it: each gets no key, which is no error, and the rest keep theirs. Nothing of a
// package outside the standard library is looked into, not even an import map holding no package.
func TestAPackageOutsideTheStandardLibraryGetsNoKey(test *testing.T) {
	test.Parallel()

	for name, outside := range map[string]struct {
		change  func(test *testing.T, graph map[string]*packages.Package)
		without []string
	}{
		"a file outside the root": {func(test *testing.T, graph map[string]*packages.Package) {
			outsideFile := filepath.Join(test.TempDir(), "print_extra.go")
			if writeError := os.WriteFile(outsideFile, []byte("package fmt\n"), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			graph[fmtId].GoFiles = append(graph[fmtId].GoFiles, outsideFile)
		}, []string{fmtId, httpId}},
		"a file named relative to the root": {func(_ *testing.T, graph map[string]*packages.Package) {
			graph[fmtId].GoFiles = []string{filepath.Join("src", "fmt", "print.go")}
		}, []string{fmtId, httpId}},
		"a module": {func(_ *testing.T, graph map[string]*packages.Package) {
			graph[errorsId].Module = &packages.Module{Path: "example.com/errors", Version: "v1.0.0"}
		}, []string{errorsId, fmtId, netId, httpId}},
		"a module's package with a broken import map": {func(_ *testing.T, graph map[string]*packages.Package) {
			graph["example.com/project"] = &packages.Package{
				ID: "example.com/project", PkgPath: "example.com/project", Name: "project",
				Module:  &packages.Module{Path: "example.com/project"},
				Imports: map[string]*packages.Package{"fmt": graph[fmtId], "example.com/missing": nil},
			}
		}, nil},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			goroot, graph := newStandardTree(test)
			keys := keyStandard(test, graph, standardContextAt(goroot))
			outside.change(test, graph)
			after := keyStandard(test, graph, standardContextAt(goroot))
			expectChanged(test, name, keys, after, outside.without...)
			for _, id := range outside.without {
				if _, keyed := after[id]; keyed {
					test.Errorf("%s has a key, want none", id)
				}
			}
			if _, keyed := after["example.com/project"]; keyed {
				test.Error("a module's package has a key, want none")
			}
		})
	}
}

// unsafe is keyed by its path alone, as Keys keys it: its file is never read.
func TestUnsafeIsKeyedByItsPathAloneInTheStandardLibrary(test *testing.T) {
	test.Parallel()

	goroot, graph := newStandardTree(test)
	keys := keyStandard(test, graph, standardContextAt(goroot))
	graph[unsafeId].GoFiles = []string{filepath.Join(goroot, "src", "unsafe", "missing.go")}
	expectChanged(test, "unsafe's file", keys, keyStandard(test, graph, standardContextAt(goroot)))
}

// No release stamp, or a root that is not absolute, keys nothing, and a standard-library file that cannot be read is
// an error naming it, as in Keys.
func TestStandardLibraryKeysRefuseWhatTheyCannotVouchFor(test *testing.T) {
	test.Parallel()

	goroot, graph := newStandardTree(test)
	unstamped := standardContextAt(goroot)
	unstamped.ReleaseStamp = ""
	relative := standardContextAt(goroot)
	relative.Goroot = filepath.Join("go", "root")
	for name, refused := range map[string]StandardLibraryContext{"no release stamp": unstamped,
		"a relative root": relative} {
		keys, keysError := StandardLibraryKeys([]*packages.Package{graph[httpId]}, refused, FileDigests{})
		if keysError == nil {
			test.Errorf("%s: want an error, got %d keys", name, len(keys))
		}
	}

	missing := filepath.Join(goroot, "src", "fmt", "missing.go")
	graph[fmtId].GoFiles = append(graph[fmtId].GoFiles, missing)
	_, keysError := StandardLibraryKeys([]*packages.Package{graph[httpId]}, standardContextAt(goroot),
		FileDigests{})
	if keysError == nil || !strings.Contains(keysError.Error(), missing) {
		test.Errorf("want an error naming %s, got %v", missing, keysError)
	}
}

// reachesCgoFiles is a module whose one package imports the standard-library packages with cgo files on linux.
var reachesCgoFiles = map[string]string{
	"go.mod":     "module example.com/reaches\n\ngo 1.21\n",
	"reaches.go": "package reaches\n\nimport (\n\t_ \"net\"\n\t_ \"os/user\"\n\t_ \"plugin\"\n)\n",
}

// reachesCgoEnvironment is what the module is loaded and listed under: linux with cgo on, where net, os/user,
// plugin and runtime/cgo have cgo files, and no go flags, so the load is the same on every machine. The load fakes
// package "C", so no C compiler is needed.
var reachesCgoEnvironment = []string{"GOWORK=off", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1", "GOFLAGS="}

// hasCgoFiles says of every standard-library package a real load reaches what go list says of its cgo files. Either
// lister gives the load the same listing, field for field (load's TestInProcessListingIsGoListsAnswer), so the
// signal holds whichever listed. No standard-library package imports runtime/cgo by hand today; one that did would
// read as having cgo files, which only puts the loader in its key, and this test would want loosening for it.
func TestHasCgoFilesIsWhatGoListSaysOfEachStandardPackage(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for name, contents := range reachesCgoFiles {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	loaded, loadError := load.Packages(directory, []string{"./..."}, reachesCgoEnvironment, nil)
	if loadError != nil {
		test.Fatalf("loading the module: %v", loadError)
	}

	build_inputs.Record(test, directory, "./...")
	command := exec.Command("go", "list", "-deps", "-json=ImportPath,Standard,CgoFiles", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), reachesCgoEnvironment...)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		test.Fatalf("listing the module: %v\n%s", runError, standardError.String())
	}
	cgoFiles := map[string]bool{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var listed struct {
			ImportPath string
			Standard   bool
			CgoFiles   []string
		}
		decodeError := decoder.Decode(&listed)
		if errors.Is(decodeError, io.EOF) {
			break
		}
		if decodeError != nil {
			test.Fatalf("reading go list's answer: %v", decodeError)
		}
		if listed.Standard {
			cgoFiles[listed.ImportPath] = len(listed.CgoFiles) > 0
		}
	}
	for _, want := range []string{"net", "os/user", "plugin", "runtime/cgo"} {
		if !cgoFiles[want] {
			test.Errorf("go list says %s has no cgo files, so this test proves less than it says", want)
		}
	}

	compared := 0
	packages.Visit(loaded.Packages, nil, func(visited *packages.Package) {
		listedCgo, standard := cgoFiles[visited.PkgPath]
		if !standard || visited.Module != nil {
			return
		}
		compared++
		if hasCgoFiles(visited) != listedCgo {
			test.Errorf("%s: hasCgoFiles says %t, go list's CgoFiles say %t", visited.PkgPath, hasCgoFiles(visited),
				listedCgo)
		}
	})
	if compared != len(cgoFiles) {
		test.Errorf("compared %d standard-library packages the load reached, go list listed %d", compared,
			len(cgoFiles))
	}
}

// The standard library's keys read the digests Keys hashed earlier in the run rather than hashing a file again: with
// every file gone once Keys has run, they key the tree as before from Keys' digests, while without them they cannot
// read a file at all.
func TestStandardLibraryKeysHashNoFileKeysHashed(test *testing.T) {
	test.Parallel()

	goroot, graph := newStandardTree(test)
	want := keyStandard(test, graph, standardContextAt(goroot))
	keyed, keysError := Keys([]*packages.Package{graph[httpId]}, Context{FormatVersion: "cache tests"}, nil,
		FileDigests{}, nil)
	if keysError != nil {
		test.Fatalf("keying the tree: %v", keysError)
	}
	reachable := make([]*packages.Package, 0, len(graph))
	removed := map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(graph)) {
		reachable = append(reachable, graph[id])
		for _, files := range fileLists(graph[id]) {
			for _, file := range files {
				if removed[file] {
					continue
				}
				removed[file] = true
				if removeError := os.Remove(file); removeError != nil {
					test.Fatalf("removing %s: %v", file, removeError)
				}
			}
		}
	}
	keys, standardError := StandardLibraryKeys(reachable, standardContextAt(goroot), keyed.Files)
	if standardError != nil {
		test.Fatalf("want the tree keyed from Keys' digests, got %v", standardError)
	}
	expectChanged(test, "keying from Keys' digests", want, keysById(keys))
	if _, unreadError := StandardLibraryKeys(reachable, standardContextAt(goroot), FileDigests{}); unreadError == nil {
		test.Error("want the removed files unreadable without Keys' digests")
	}
}
