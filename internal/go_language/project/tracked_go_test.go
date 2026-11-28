package project

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/testing/git_index_testing"
)

// hiddenFromGo are tracked files go never reads as the root module's (#td4wp62), each by the rule the census's walk
// leaves it out by, with the files on disk that make each so.
var hiddenFromGo = map[string]string{
	"go.mod":                    "module example.com/witness\n\ngo 1.27\n\nignore ./generated\n",
	"README.md":                 "",
	"_underscore.go":            "",
	".dot.go":                   "",
	"testdata/fixture.go":       "",
	"deep/testdata/fixture.go":  "",
	"_private/hidden.go":        "",
	".hidden/hidden.go":         "",
	"vendor/example.com/x/x.go": "",
	"generated/generated.go":    "",
	"nested/go.mod":             "module example.com/nested\n\ngo 1.27\n",
	"nested/nested.go":          "",
	"clone/.git":                "gitdir: elsewhere\n",
	"clone/cloned.go":           "",
	"replaced.go/inside.txt":    "",
}

// writeWitnessTree writes files under a fresh root, an index tracking each of them and each of deleted, which it never
// writes, and returns the root and the module workspace at it.
func writeWitnessTree(test *testing.T, files map[string]string, deleted ...string) (string, Workspace) {
	test.Helper()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	if writeError := writeFiles(root, files); writeError != nil {
		test.Fatal(writeError)
	}
	tracked := append([]string{}, deleted...)
	for name := range files {
		tracked = append(tracked, name)
	}
	if writeError := git_index_testing.Write(filepath.Join(root, ".git"), tracked...); writeError != nil {
		test.Fatal(writeError)
	}
	return root, Workspace{Root: root, Modules: []Module{{Directory: "."}}}
}

// The second witness reads what go would see from the index alone (#td4wp62), so on an honest tree it agrees with the
// census: a module whose every tracked file go leaves out, by name, below testdata, a `.` or `_` directory, a
// directory below vendor, an `ignore` directive, a nested module or a repository of its own, or deleted from the disk
// or replaced there by a directory, holds none. Each file go does read is found: one at the module's root, and
// one in vendor's own directory, whose files go matches as a package.
func TestTheTrackedWitnessSeesWhatGoSees(test *testing.T) {
	test.Parallel()

	_, hidden := writeWitnessTree(test, hiddenFromGo, "deleted.go", "replaced.go")
	if found, witnessError := TrackedGoFile(hidden); witnessError != nil || found != "" {
		test.Errorf("over files go never reads the witness found %q (%v), want none", found, witnessError)
	}
	for name, want := range map[string]string{"kept.go": "kept.go", "vendor/modules.go": "vendor/modules.go"} {
		files := map[string]string{name: ""}
		for hiddenName, contents := range hiddenFromGo {
			files[hiddenName] = contents
		}
		root, workspace := writeWitnessTree(test, files)
		found, witnessError := TrackedGoFile(workspace)
		if witnessError != nil || found != filepath.Join(root, filepath.FromSlash(want)) {
			test.Errorf("with %s tracked the witness found %q (%v), want it", name, found, witnessError)
		}
	}
}

// A workspace's witness reads every module it uses (#td4wp62): a file tracked in its second module is found though
// the first holds none, and a module the workspace never uses is never read.
func TestTheTrackedWitnessReadsEveryModuleAWorkspaceUses(test *testing.T) {
	test.Parallel()

	root, _ := writeWitnessTree(test, map[string]string{
		"go.work":         "go 1.27\n\nuse (\n\t./first\n\t./second\n)\n",
		"first/go.mod":    "module example.com/first\n\ngo 1.27\n",
		"second/go.mod":   "module example.com/second\n\ngo 1.27\n",
		"second/kept.go":  "",
		"unused/go.mod":   "module example.com/unused\n\ngo 1.27\n",
		"unused/also.go":  "",
		"first/README.md": "",
	})
	workspace := Workspace{
		Root: root, FromWorkspace: true, Modules: []Module{{Directory: "first"}, {Directory: "second"}},
	}
	found, witnessError := TrackedGoFile(workspace)
	if witnessError != nil || found != filepath.Join(root, "second", "kept.go") {
		test.Errorf("the witness found %q (%v), want second/kept.go", found, witnessError)
	}
	workspace.Modules = workspace.Modules[:1]
	if found, witnessError := TrackedGoFile(workspace); witnessError != nil || found != "" {
		test.Errorf("over the first module alone the witness found %q (%v), want none", found, witnessError)
	}
}

// With no repository there is no second witness (#td4wp62): the witness says so as an error rather than reading none.
func TestTheTrackedWitnessOfAModuleInNoRepositoryIsAnError(test *testing.T) {
	test.Parallel()

	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	goMod := map[string]string{"go.mod": "module example.com/alone\n\ngo 1.27\n"}
	if writeError := writeFiles(root, goMod); writeError != nil {
		test.Fatal(writeError)
	}
	alone := Workspace{Root: root, Modules: []Module{{Directory: "."}}}
	if _, witnessError := TrackedGoFile(alone); witnessError == nil {
		test.Error("a module in no repository read a witness, want an error")
	}
}
