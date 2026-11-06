package load

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// listingFiles are a project module of three packages and a module beside it the project replaces by path, outside
// its directory: base, which embeds a file and has a test, user, which imports base and the replaced module's lib,
// and lib itself.
var listingFiles = map[string]string{
	"project/go.mod": "module example.com/listing\n\ngo 1.24\n\nrequire example.com/outside v0.0.0\n\n" +
		"replace example.com/outside => ../outside\n",
	"project/base/base.go": "package base\n\n// Base is one.\nfunc Base() int { return 1 }\n",
	"project/base/base_test.go": "package base\n\nimport \"testing\"\n\n// TestBase calls Base.\n" +
		"func TestBase(test *testing.T) {\n\ttest.Parallel()\n\t_ = Base()\n}\n",
	"project/base/embed.go": "package base\n\nimport \"embed\"\n\n// Static is the files base embeds.\n" +
		"//\n//go:embed static/*\nvar Static embed.FS\n",
	"project/base/static/first.txt": "first\n",
	"project/user/user.go": "package user\n\nimport (\n\t\"example.com/listing/base\"\n" +
		"\t\"example.com/outside/lib\"\n)\n\n// User calls Base and Lib.\n" +
		"func User() int { return base.Base() + lib.Lib() }\n",
	"outside/go.mod":     "module example.com/outside\n\ngo 1.24\n",
	"outside/lib/lib.go": "package lib\n\n// Lib is two.\nfunc Lib() int { return 2 }\n",
}

// writeListingFiles writes files into root.
func writeListingFiles(test *testing.T, root string, files map[string]string) {
	test.Helper()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
}

// loadSummary is what a load of directory under environment says of its packages and diagnostics, one line each,
// sorted, so two loads compare.
func loadSummary(test *testing.T, directory string, environment []string, listings ListingStore) ([]string, bool) {
	test.Helper()
	loaded, loadError := PackagesCached(directory, []string{"./..."}, environment, nil, Caching{Listings: listings})
	if loadError != nil {
		test.Fatalf("loading %s: %v", directory, loadError)
	}
	var summary []string
	for _, loadedPackage := range loaded.Packages {
		imports := make([]string, 0, len(loadedPackage.Imports))
		for path, imported := range loadedPackage.Imports {
			imports = append(imports, path+"="+imported.ID)
		}
		slices.Sort(imports)
		summary = append(summary, fmt.Sprintf("%s files %v embeds %v ignored %v imports %v", loadedPackage.ID,
			loadedPackage.GoFiles, loadedPackage.EmbedFiles, loadedPackage.IgnoredFiles, imports))
	}
	for _, diagnostic := range loaded.Diagnostics {
		summary = append(summary, fmt.Sprintf("diagnostic %+v", diagnostic))
	}
	slices.Sort(summary)
	return summary, loaded.ListingKept
}

// listingEdit is one change to the project after go list's answer was kept, and whether the answer may be kept
// through it.
type listingEdit struct {
	name string

	// files are written, removed are deleted, both relative to the directory holding the project and the module
	// beside it.
	files   map[string]string
	removed []string

	// workspace has the project load through a go.work that uses it, rather than with `GOWORK=off`.
	workspace bool

	// environment is added to the edited load's, as another target.
	environment []string

	kept bool
}

// listingEdits are one edit of each kind go list reads, and one it does not.
var listingEdits = []listingEdit{
	{name: "a body edited", kept: true, files: map[string]string{
		"project/base/base.go": "package base\n\n// Base is two.\nfunc Base() int { return 2 }\n\n// Added below.\n",
	}},
	{name: "an import added", files: map[string]string{
		"project/base/base.go": "package base\n\nimport \"strings\"\n\n// Base is one.\n" +
			"func Base() int { return len(strings.Repeat(\"a\", 1)) }\n",
	}},
	{name: "a build constraint added", files: map[string]string{
		"project/base/base_test.go": "//go:build ignore\n\n" + listingFiles["project/base/base_test.go"],
	}},
	{name: "a Go file added", files: map[string]string{
		"project/base/more.go": "package base\n\n// More is two.\nfunc More() int { return 2 }\n",
	}},
	{name: "a Go file removed", removed: []string{"project/base/base_test.go"}},
	{name: "an assembly file added", files: map[string]string{
		"project/user/stub.s": "// An assembly file go list names among the package's files.\n",
	}},
	{name: "a package added", files: map[string]string{
		"project/extra/extra.go": "package extra\n\n// Extra is three.\nfunc Extra() int { return 3 }\n",
	}},
	{name: "a package directory renamed", removed: []string{"project/user/user.go", "project/user"},
		files: map[string]string{"project/client/user.go": listingFiles["project/user/user.go"]}},
	{name: "an embed target added", files: map[string]string{"project/base/static/second.txt": "second\n"}},
	{name: "a replace changed", files: map[string]string{
		"project/go.mod":         strings.Replace(listingFiles["project/go.mod"], "../outside", "../moved", 1),
		"moved/go.mod":           listingFiles["outside/go.mod"],
		"moved/lib/lib.go":       listingFiles["outside/lib/lib.go"],
		"moved/lib/elsewhere.go": "package lib\n\n// Elsewhere is four.\nfunc Elsewhere() int { return 4 }\n",
	}},
	{name: "a file added in the replaced module", files: map[string]string{
		"outside/lib/more.go": "package lib\n\n// More is three.\nfunc More() int { return 3 }\n",
	}},
	{name: "the go version changed", files: map[string]string{
		"project/go.mod": strings.Replace(listingFiles["project/go.mod"], "go 1.24", "go 1.25", 1),
	}},
	{name: "a go.work use added", workspace: true, files: map[string]string{
		"project/go.work": "go 1.24\n\nuse (\n\t.\n\t../outside\n)\n",
	}},
	{name: "another target", environment: []string{"GOOS=linux", "GOARCH=amd64"}},
}

// go list's answer is kept until something it reads moves. An edit below a file's imports keeps it; each kind of
// edit go list reads asks it again: an import, a build constraint, a Go or assembly file added or removed, a package
// added or renamed, a new file an embed matches, a replace changed or a file added in the module it names outside the
// project, the module's go version, a go.work use, or another target. Either way the load is the one a load
// without a kept answer gives.
func TestGoListsAnswerIsKeptUntilWhatItReadsMoves(test *testing.T) {
	test.Parallel()

	for _, edit := range listingEdits {
		test.Run(edit.name, func(test *testing.T) {
			test.Parallel()

			root, resolveError := filepath.EvalSymlinks(test.TempDir())
			if resolveError != nil {
				test.Fatalf("resolving the temporary directory: %v", resolveError)
			}
			writeListingFiles(test, root, listingFiles)
			project := filepath.Join(root, "project")
			environment := []string{"GOWORK=off"}
			if edit.workspace {
				writeListingFiles(test, root, map[string]string{"project/go.work": "go 1.24\n\nuse .\n"})
				environment = nil
			}
			listings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
			if _, kept := loadSummary(test, project, environment, listings); kept {
				test.Fatalf("want the first load to ask go list")
			}
			if _, kept := loadSummary(test, project, environment, listings); !kept {
				test.Fatalf("want the second load, with nothing moved, to keep go list's answer")
			}
			for _, removed := range edit.removed {
				if removeError := os.Remove(filepath.Join(root, filepath.FromSlash(removed))); removeError != nil {
					test.Fatalf("removing %s: %v", removed, removeError)
				}
			}
			writeListingFiles(test, root, edit.files)
			editedEnvironment := append(slices.Clone(environment), edit.environment...)
			edited, kept := loadSummary(test, project, editedEnvironment, listings)
			if kept != edit.kept {
				test.Errorf("want the answer kept %t after the edit, got %t", edit.kept, kept)
			}
			fresh, _ := loadSummary(test, project, editedEnvironment, nil)
			if strings.Join(edited, "\n") != strings.Join(fresh, "\n") {
				test.Fatalf("after the edit\n%s\nwithout a kept answer\n%s", strings.Join(edited, "\n"),
					strings.Join(fresh, "\n"))
			}
		})
	}
}

// A listed package's directory is left out of the fingerprint only when it cannot move: in the module cache under a
// module with a version, or in the toolchain's root in no module. A directory there that is neither, a replace into
// the module cache by path or a module under the toolchain's root, is read like the workspace's, and a listing with
// an error in it is never kept.
func TestOnlyWhatCannotMoveIsLeftOutOfTheListing(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	moduleCache := filepath.Join(root, "modules")
	toolchainRoot := filepath.Join(root, "toolchain")
	for _, directory := range []string{"modules/versioned", "modules/replaced", "toolchain/standard",
		"toolchain/moduled", "workspace"} {
		if makeError := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o755); makeError != nil {
			test.Fatalf("making %s: %v", directory, makeError)
		}
	}
	listed := &listing{roots: map[string]bool{}, testOnly: map[string]bool{}, byPath: map[string]*listedPackage{}}
	listed.packages = []*listedPackage{
		{ImportPath: "versioned", Directory: filepath.Join(moduleCache, "versioned"),
			Module: &packages.Module{Path: "versioned", Version: "v1.0.0"}},
		{ImportPath: "replaced", Directory: filepath.Join(moduleCache, "replaced"),
			Module: &packages.Module{Path: "replaced", Replace: &packages.Module{Path: "../replaced"}}},
		{ImportPath: "standard", Directory: filepath.Join(toolchainRoot, "standard")},
		{ImportPath: "moduled", Directory: filepath.Join(toolchainRoot, "moduled"),
			Module: &packages.Module{Path: "moduled"}},
	}
	kept, keepable := keepListing(filepath.Join(root, "workspace"), moduleCache, toolchainRoot, listed)
	if !keepable {
		test.Fatalf("want a listing without errors kept")
	}
	want := []string{filepath.Join(moduleCache, "replaced"), filepath.Join(toolchainRoot, "moduled")}
	if !slices.Equal(kept.Directories, want) {
		test.Fatalf("want only what can move read, %q, got %q", want, kept.Directories)
	}
	listed.packages[0].Error = &listedError{Message: "cannot find module"}
	if _, keepable := keepListing(filepath.Join(root, "workspace"), moduleCache, toolchainRoot, listed); keepable {
		test.Fatalf("want a listing with an error never kept")
	}
}

// A module a go.work uses through a symbolic link, as every worktree's go.work reaches TypeScript/tsc, is still read:
// the walk names the link and does not follow it, as go does not, but each listed package's directory is read
// through it. So a header edit inside the linked module asks go list again, and the load is the one without a kept
// answer.
func TestAModuleAGoWorkUsesThroughASymbolicLinkIsRead(test *testing.T) {
	test.Parallel()

	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	writeListingFiles(test, root, map[string]string{
		"project/go.mod":  "module example.com/linked\n\ngo 1.24\n",
		"project/go.work": "go 1.24\n\nuse (\n\t.\n\t./outside\n)\n",
		"project/user/user.go": "package user\n\nimport \"example.com/outside/lib\"\n\n// User calls Lib.\n" +
			"func User() int { return lib.Lib() }\n",
		"elsewhere/go.mod":     listingFiles["outside/go.mod"],
		"elsewhere/lib/lib.go": listingFiles["outside/lib/lib.go"],
	})
	project := filepath.Join(root, "project")
	if linkError := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(project, "outside")); linkError != nil {
		test.Fatalf("linking the module: %v", linkError)
	}
	listings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
	loadSummary(test, project, nil, listings)
	if _, kept := loadSummary(test, project, nil, listings); !kept {
		test.Fatalf("want the second load, with nothing moved, to keep go list's answer")
	}
	writeListingFiles(test, root, map[string]string{
		"elsewhere/lib/lib.go": "package lib\n\nimport \"strings\"\n\n// Lib is two.\n" +
			"func Lib() int { return len(strings.Repeat(\"a\", 2)) }\n",
	})
	edited, kept := loadSummary(test, project, nil, listings)
	if kept {
		test.Fatalf("want a header edit inside the linked module to ask go list again")
	}
	fresh, _ := loadSummary(test, project, nil, nil)
	if strings.Join(edited, "\n") != strings.Join(fresh, "\n") {
		test.Fatalf("after the edit\n%s\nwithout a kept answer\n%s", strings.Join(edited, "\n"),
			strings.Join(fresh, "\n"))
	}
}
