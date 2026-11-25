package guard

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// moduleShelf is the folder every module of the repository's own, other than the root, sits directly under.
const moduleShelf = "modules"

// typeScriptModules are the TypeScript fork, a submodule holding upstream's modules, and the shims generated over
// it, each a module of its own, which keep their go.mod files outside the shelf. The libraries move (#j2k92bh)
// replaces both entries with libraries/, and since each must exist, that move has to edit this list.
var typeScriptModules = []string{"TypeScript", "TypeScript-shim"}

// The Go modules other repositories pin by tag live in modules/, and the rule is mechanical: a folder is in modules/
// if and only if it has its own go.mod (#gcdfx7v). What they share is that each is versioned on its own and tagged
// modules/<name>/v..., so a module anywhere else has a tag prefix that names the wrong path, and a folder on the shelf
// without a go.mod is a package of the root module that no other repository can pin.
//
// So every folder directly under modules/ holds a go.mod, and no go.mod exists anywhere else except the root's, the
// fixtures under any testdata/ (Go's grammar for a module a test loads), and the TypeScript fork's and its shims'.
// Directories whose name begins with a dot, `.cache` among them with its worktrees of other trees, and npm's
// node_modules are not the repository's, and are not read.
func TestEveryModuleOfItsOwnIsOnTheShelf(test *testing.T) {
	test.Parallel()
	// This package sits three directories below the module root.
	root, absolutePathError := filepath.Abs(filepath.Join("..", "..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	for _, exception := range typeScriptModules {
		if _, statError := os.Stat(filepath.Join(root, exception)); statError != nil {
			test.Errorf("%s, a named exception to the shelf, is gone (%v): the move that took it replaces its "+
				"entry in typeScriptModules with where it went", exception, statError)
		}
	}
	shelf := readModuleShelf(test, root)
	// A shelf read from the wrong root would find nothing missing and nothing stray, so it has to hold modules.
	if len(shelf.modules) == 0 {
		test.Fatalf("found no module in %s/, so a clean answer would mean nothing", moduleShelf)
	}
	for _, folder := range shelf.withoutGoModule {
		test.Errorf("%s is on the shelf with no go.mod of its own: a package of the root module belongs under "+
			"internal/, and a module other repositories pin needs its go.mod", folder)
	}
	for _, folder := range shelf.stray {
		test.Errorf("%s has a go.mod of its own outside %s/: a module other repositories pin by tag moves to "+
			"%s/<name>, and one nothing outside cohere imports is a package of the root module", folder, moduleShelf,
			moduleShelf)
	}
}

// The guard sees what it is for, on a tree of its own: a stray module beside the shelf and one nested inside a
// shelved module are each named, as is a shelf folder with no go.mod, and the root's, a fixture's, the TypeScript
// shims' and a dot directory's go.mod are not.
func TestTheModuleShelfGuardSeesAStrayAndAMissingModule(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	goModule := []byte("module example.com/fixture\n\ngo 1.27\n")
	for _, file := range []string{
		"go.mod",
		"modules/kept/go.mod",
		"modules/kept/inner/go.mod",
		"modules/unmoduled/unmoduled.go",
		"command/stray/go.mod",
		"internal/fixture/testdata/module/go.mod",
		"TypeScript-shim/ast/go.mod",
		".cache/worktree/go.mod",
	} {
		path := filepath.Join(root, filepath.FromSlash(file))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, goModule, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	shelf := readModuleShelf(test, root)
	if want := []string{"modules/kept"}; !slices.Equal(shelf.modules, want) {
		test.Errorf("the shelf's modules are %q, want %q", shelf.modules, want)
	}
	if want := []string{"modules/unmoduled"}; !slices.Equal(shelf.withoutGoModule, want) {
		test.Errorf("the shelf folders without a go.mod are %q, want %q", shelf.withoutGoModule, want)
	}
	if want := []string{"command/stray", "modules/kept/inner"}; !slices.Equal(shelf.stray, want) {
		test.Errorf("the stray modules are %q, want %q", shelf.stray, want)
	}
}

// moduleShelfReading is what readModuleShelf found, each folder slash-separated and relative to the root, sorted.
type moduleShelfReading struct {
	// modules are the shelf's folders that hold a go.mod.
	modules []string
	// withoutGoModule are the shelf's folders that hold none.
	withoutGoModule []string
	// stray are the folders outside the shelf, other than the root, a fixture or the TypeScript fork's, that hold
	// one.
	stray []string
}

// readModuleShelf reads the shelf under root and walks the rest of root for a go.mod anywhere else.
func readModuleShelf(test *testing.T, root string) moduleShelfReading {
	test.Helper()
	var reading moduleShelfReading
	entries, readError := os.ReadDir(filepath.Join(root, moduleShelf))
	if readError != nil {
		test.Fatal(readError)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		folder := moduleShelf + "/" + entry.Name()
		if _, statError := os.Stat(filepath.Join(root, moduleShelf, entry.Name(), "go.mod")); statError == nil {
			reading.modules = append(reading.modules, folder)
		} else {
			reading.withoutGoModule = append(reading.withoutGoModule, folder)
		}
	}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		native, relativeError := filepath.Rel(root, path)
		if relativeError != nil {
			return relativeError
		}
		relative := filepath.ToSlash(native)
		if entry.IsDir() {
			if relative != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" ||
				entry.Name() == "testdata" || slices.Contains(typeScriptModules, relative)) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "go.mod" {
			return nil
		}
		folder := filepath.ToSlash(filepath.Dir(native))
		if folder == "." || slices.Contains(reading.modules, folder) {
			return nil
		}
		reading.stray = append(reading.stray, folder)
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	slices.Sort(reading.modules)
	slices.Sort(reading.withoutGoModule)
	slices.Sort(reading.stray)
	return reading
}
