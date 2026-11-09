package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/build_inputs"
)

// The census of a workspace is every `.go` file go would see in its owned modules, as absolute paths: inner
// walked as itself, and nothing from testdata, a dot or underscore directory, below vendor, a path an
// `ignore` directive names, the unowned nested module, or the submodule. A file a `.gitignore` names, at the
// root or in inner, or one under an ignored directory, is in the workspace and not ours, named with the line that
// left it out: the directory's own for a file under an ignored directory.
func TestTakeCensusListsWhatGoSeesAndExcludesWhatGitIgnores(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	covered, readError := ReadWorkspace(built.repository)
	if readError != nil {
		test.Fatal(readError)
	}
	census, censusError := TakeCensus(covered)
	if censusError != nil {
		test.Fatal(censusError)
	}
	absolute := func(names ...string) []string {
		paths := make([]string, 0, len(names))
		for _, name := range names {
			paths = append(paths, filepath.Join(built.repository, filepath.FromSlash(name)))
		}
		return paths
	}

	ours := absolute("deep/deep.go", "deep/generated/kept.go", "inner/deeper/deeper.go", "inner/inner.go",
		"main.go", "tools/tool.go", "vendor/vendored.go")
	ignored := absolute("build/generated.go", "inner/local.go", "scratch.go")
	sources := []string{".gitignore:2", "inner/.gitignore:1", ".gitignore:1"}
	if !slices.Equal(census.Ours, ours) {
		test.Errorf("ours are %q, want %q", census.Ours, ours)
	}
	files := slices.Sorted(slices.Values(append(slices.Clone(ours), ignored...)))
	if !slices.Equal(census.Files, files) {
		test.Errorf("the workspace's files are %q, want %q", census.Files, files)
	}
	excluded := make([]Exclusion, 0, len(ignored))
	for index, file := range ignored {
		excluded = append(excluded, Exclusion{File: file, Reason: IgnoredByGit, Source: sources[index]})
	}
	if !slices.Equal(census.Excluded, excluded) {
		test.Errorf("excluded are %v, want %v", census.Excluded, excluded)
	}
}

// Each directory go does not enter is a file the census must not count, so each is its own case: a census that
// entered any one of them would count its file.
func TestTakeCensusDoesNotEnterWhatGoDoesNot(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	covered, readError := ReadWorkspace(built.repository)
	if readError != nil {
		test.Fatal(readError)
	}
	census, censusError := TakeCensus(covered)
	if censusError != nil {
		test.Fatal(censusError)
	}
	skipped := map[string]string{
		"a testdata directory":          "testdata/fixture/fixture.go",
		"a testdata directory below":    "inner/deeper/testdata/skipped.go",
		"a dot directory":               ".hidden/hidden.go",
		"an underscore directory":       "_scratch/underscore.go",
		"a module go.work does not use": "nested/nested.go",
		"a submodule":                   "library/mod/library.go",
		"a directory below vendor":      "vendor/library/library.go",
		"an anchored ignore directive":  "generated/generated.go",
		"an ignore directive's path":    "tools/scripts/script.go",
		"an ignore directive deeper":    "deep/tools/scripts/script.go",
		"a nested ignore directive":     "deep/inside/inside.go",
		"below a nested ignore":         "deep/inside/below/below.go",
		"an underscore file":            "_draft.go",
		"a dot file":                    ".draft.go",
		"a directory of only those":     "drafts/_only.go",
	}
	for name, file := range skipped {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			if slices.Contains(census.Files, filepath.Join(built.repository, filepath.FromSlash(file))) {
				test.Errorf("the census counted %s", file)
			}
		})
	}

	// What ./... does match beside them: vendor's own package, the ignored path's parent, and a directory of
	// the anchored name below the module's root.
	counted := map[string]string{
		"a vendor directory's own files":  "vendor/vendored.go",
		"the parent of an ignored path":   "tools/tool.go",
		"an anchored name below the root": "deep/generated/kept.go",
		"the parent of a nested ignore":   "deep/deep.go",
	}
	for name, file := range counted {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			if !slices.Contains(census.Ours, filepath.Join(built.repository, filepath.FromSlash(file))) {
				test.Errorf("the census left out %s", file)
			}
		})
	}
}

// The census's directories in the root module are the ones `go list ./...` lists there, so what go leaves
// out of ./... by testdata, `.` and `_` (directories, and files: drafts holds only such files, so it is no
// package), vendor, `ignore` directives and module boundaries, the census leaves out the same way, checked
// against go rather than against this package's reading of it. The submodule is
// a module of its own, so go never counts it in the root module either.
func TestTakeCensusSeesTheRootModulesPackagesGoListSees(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	covered, readError := ReadWorkspace(built.repository)
	if readError != nil {
		test.Fatal(readError)
	}
	census, censusError := TakeCensus(covered)
	if censusError != nil {
		test.Fatal(censusError)
	}

	command := exec.Command("go", "list", "-f", `{{if eq .Module.Path "example.com/root"}}{{.Dir}}{{end}}`, "./...")
	command.Dir = build_inputs.OutsideModule(test, built.repository)
	// -mod=readonly, since a vendor directory at a go.work's root could otherwise read as the workspace's
	// vendoring, and workspace mode allows no other value.
	command.Env = append(withoutVariable(os.Environ(), "GOWORK"), "GOTOOLCHAIN=local", "GOPROXY=off",
		"GOFLAGS=-mod=readonly")
	output, listError := command.CombinedOutput()
	if listError != nil {
		test.Fatalf("go list: %v\n%s", listError, output)
	}
	// go answers from its resolved working directory, a temporary directory's symbolic links followed, so both
	// sides are compared relative to the root they were read from.
	resolvedRepository, resolveError := filepath.EvalSymlinks(built.repository)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	var listed []string
	for _, directory := range strings.Fields(string(output)) {
		relative, relativeError := filepath.Rel(resolvedRepository, directory)
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		listed = append(listed, filepath.ToSlash(relative))
	}
	slices.Sort(listed)

	var censused []string
	for _, file := range census.Files {
		relative, relativeError := filepath.Rel(built.repository, filepath.Dir(file))
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		relative = filepath.ToSlash(relative)
		if relative == "inner" || strings.HasPrefix(relative, "inner/") || slices.Contains(censused, relative) {
			continue
		}
		censused = append(censused, relative)
	}
	slices.Sort(censused)
	if !slices.Equal(censused, listed) {
		test.Errorf("the census's root-module directories are %q, and go list's are %q", censused, listed)
	}
}

// A module that is its own root below a repository's root is still governed by that repository's ignore
// files: nested's scratch.go is named by the root's `.gitignore`, one directory above the module's root.
func TestTakeCensusOfAModuleBelowTheRepositoryRootReadsTheRootsIgnoreFiles(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	nested := filepath.Join(built.repository, "nested")
	covered, readError := ReadWorkspace(nested)
	if readError != nil {
		test.Fatal(readError)
	}
	census, censusError := TakeCensus(covered)
	if censusError != nil {
		test.Fatal(censusError)
	}
	nestedFile, scratchFile := filepath.Join(nested, "nested.go"), filepath.Join(nested, "scratch.go")
	if !slices.Equal(census.Ours, []string{nestedFile}) {
		test.Errorf("ours are %q, want only %s", census.Ours, nestedFile)
	}
	want := []Exclusion{{File: scratchFile, Reason: IgnoredByGit, Source: "../.gitignore:1"}}
	if !slices.Equal(census.Excluded, want) {
		test.Errorf("excluded are %v, want %s ignored by git at ../.gitignore:1", census.Excluded, scratchFile)
	}
}

// A lone module is its one directory, and outside any repository nothing is ignored.
func TestTakeCensusOfALoneModule(test *testing.T) {
	test.Parallel()
	built := readFixture(test)
	covered, readError := ReadWorkspace(built.lone)
	if readError != nil {
		test.Fatal(readError)
	}
	census, censusError := TakeCensus(covered)
	if censusError != nil {
		test.Fatal(censusError)
	}
	lone := []string{filepath.Join(built.lone, "lone.go")}
	if !slices.Equal(census.Files, lone) || !slices.Equal(census.Ours, lone) || len(census.Excluded) != 0 {
		test.Errorf("the lone module's census is %+v, want %s alone and ours", census, lone[0])
	}
}
