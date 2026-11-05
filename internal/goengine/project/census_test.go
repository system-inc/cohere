package project

import (
	"path/filepath"
	"slices"
	"testing"
)

// The census of a workspace is every `.go` file go would see in its owned modules, as absolute paths: inner
// walked as itself, and nothing from testdata, a dot or underscore directory, the unowned nested module, or the
// submodule. A file a `.gitignore` names, at the root or in inner, or one under an ignored directory, is in the
// workspace and not ours.
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

	ours := absolute("inner/deeper/deeper.go", "inner/inner.go", "main.go")
	ignored := absolute("build/generated.go", "inner/local.go", "scratch.go")
	if !slices.Equal(census.Ours, ours) {
		test.Errorf("ours are %q, want %q", census.Ours, ours)
	}
	files := slices.Sorted(slices.Values(append(slices.Clone(ours), ignored...)))
	if !slices.Equal(census.Files, files) {
		test.Errorf("the workspace's files are %q, want %q", census.Files, files)
	}
	excluded := make([]Exclusion, 0, len(ignored))
	for _, file := range ignored {
		excluded = append(excluded, Exclusion{File: file, Reason: IgnoredByGit})
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
	}
	for name, file := range skipped {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			if slices.Contains(census.Files, filepath.Join(built.repository, filepath.FromSlash(file))) {
				test.Errorf("the census counted %s", file)
			}
		})
	}
}

// A module that is its own root below a repository's root is still governed by that repository's ignore
// files: nested's scratch.go is named by the root's `.gitignore`.
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
	if !slices.Equal(census.Excluded, []Exclusion{{File: scratchFile, Reason: IgnoredByGit}}) {
		test.Errorf("excluded are %v, want %s ignored by git", census.Excluded, scratchFile)
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
