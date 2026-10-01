package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree writes files under a directory, creating whatever directories they need.
func writeTree(t *testing.T, directory string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestLocateProjectWalksUpToTheNearestTsconfig holds discovery, and each flag that overrides it.
//
// The defect was that every default resolved against the working directory, so `cohere` worked from
// the root and nowhere else: from `modules/tasks` it reported "no tsconfig at
// modules/tasks/tsconfig.json", which is true and useless. Each case below is one sentence of the
// precedence locateProject states, asserted on the value that decides it.
func TestLocateProjectWalksUpToTheNearestTsconfig(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"tsconfig.json":                 "{}",
		"CohereSettings.json":           "{}",
		"modules/tasks/source/A.ts":     "",
		"projects/nested/tsconfig.json": "{}",
	})
	deep := filepath.Join(root, "modules", "tasks", "source")

	t.Run("discovered from a subdirectory", func(t *testing.T) {
		location, err := locateProject(locationRequest{
			WorkingDirectory:   deep,
			ConfigFileName:     projectMarker,
			LintConfigFileName: "CohereSettings.json",
		})
		if err != nil {
			t.Fatal(err)
		}
		if location.Root != root {
			t.Errorf("root = %s, want %s", location.Root, root)
		}
		if location.ConfigFileName != filepath.Join(root, "tsconfig.json") {
			t.Errorf("tsconfig = %s", location.ConfigFileName)
		}
		if location.LintConfigFileName != filepath.Join(root, "CohereSettings.json") {
			t.Errorf("a default lint config resolved somewhere other than the root: %s", location.LintConfigFileName)
		}
		// Typed paths stay where they were typed. Resolving them against the root would make
		// `cohere A.ts` from `source` look for a file at the top of the tree.
		if location.ArgumentBase != deep {
			t.Errorf("typed paths resolve against %s, want the working directory %s", location.ArgumentBase, deep)
		}
		if note := location.rootNote(); !strings.Contains(note, root) || !strings.Contains(note, deep) {
			t.Errorf("a root found elsewhere did not say both where it checked and where it started: %q", note)
		}
	})

	// The control for the note: at the root it must be silent, or it prints on every ordinary run.
	t.Run("silent at the root", func(t *testing.T) {
		location, err := locateProject(locationRequest{
			WorkingDirectory: root, ConfigFileName: projectMarker, LintConfigFileName: "CohereSettings.json",
		})
		if err != nil {
			t.Fatal(err)
		}
		if note := location.rootNote(); note != "" {
			t.Errorf("a run started at its own root still carried a root note: %q", note)
		}
	})

	// The nearest tsconfig wins, which is what makes a separate sub-project its own root rather than
	// a directory of the project above it.
	t.Run("the nearest tsconfig wins", func(t *testing.T) {
		nested := filepath.Join(root, "projects", "nested")
		location, err := locateProject(locationRequest{
			WorkingDirectory: nested, ConfigFileName: projectMarker, LintConfigFileName: "CohereSettings.json",
		})
		if err != nil {
			t.Fatal(err)
		}
		if location.Root != nested {
			t.Errorf("walked past a nearer tsconfig to %s", location.Root)
		}
	})

	t.Run("--directory wins and is a cd", func(t *testing.T) {
		location, err := locateProject(locationRequest{
			WorkingDirectory:   deep,
			Directory:          "../..",
			ConfigFileName:     projectMarker,
			LintConfigFileName: "CohereSettings.json",
		})
		if err != nil {
			t.Fatal(err)
		}
		modules := filepath.Join(root, "modules")
		if location.Root != modules || location.ArgumentBase != modules {
			t.Errorf("--directory was not both the root and the base: root %s, base %s", location.Root, location.ArgumentBase)
		}
		// Discovery would have found the real tsconfig above; an explicit directory must not.
		if location.ConfigFileName != filepath.Join(modules, "tsconfig.json") {
			t.Errorf("discovery overrode an explicit --directory: %s", location.ConfigFileName)
		}
	})

	t.Run("--tsconfig wins and anchors the root", func(t *testing.T) {
		location, err := locateProject(locationRequest{
			WorkingDirectory:    deep,
			ConfigFileName:      "../../../projects/nested/tsconfig.json",
			ConfigFileNameGiven: true,
			LintConfigFileName:  "CohereSettings.json",
		})
		if err != nil {
			t.Fatal(err)
		}
		nested := filepath.Join(root, "projects", "nested")
		if location.Root != nested {
			t.Errorf("an explicit tsconfig did not anchor the root: %s", location.Root)
		}
		if location.LintConfigFileName != filepath.Join(nested, "CohereSettings.json") {
			t.Errorf("the default lint config did not follow the root: %s", location.LintConfigFileName)
		}
	})

	t.Run("a typed --lint-config resolves where it was typed", func(t *testing.T) {
		location, err := locateProject(locationRequest{
			WorkingDirectory:        deep,
			ConfigFileName:          projectMarker,
			LintConfigFileName:      "Local.json",
			LintConfigFileNameGiven: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if location.LintConfigFileName != filepath.Join(deep, "Local.json") {
			t.Errorf("a typed lint config resolved against %s", location.LintConfigFileName)
		}
	})
}

// TestLocateProjectFailsLoudlyOutsideAnyProject holds that finding nothing is an error naming where
// the walk began, never a fallback to the working directory.
func TestLocateProjectFailsLoudlyOutsideAnyProject(t *testing.T) {
	// t.TempDir is under the system temp directory, which has no tsconfig.json at or above it.
	outside := t.TempDir()
	for directory := outside; ; directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, projectMarker)); err == nil {
			t.Skipf("%s holds a tsconfig.json, so there is no outside to test from", directory)
		}
		if filepath.Dir(directory) == directory {
			break
		}
	}

	_, err := locateProject(locationRequest{
		WorkingDirectory: outside, ConfigFileName: projectMarker, LintConfigFileName: "CohereSettings.json",
	})
	if err == nil {
		t.Fatal("no tsconfig anywhere above, and locateProject returned a location")
	}
	if !strings.Contains(err.Error(), outside) || !strings.Contains(err.Error(), projectMarker) {
		t.Errorf("the error does not say what it looked for and where: %v", err)
	}
}

// TestDotFromASubdirectoryIsThatSubdirectory holds the whole-tree answer to the root rather than to
// the working directory.
//
// Before the root could be found by walking up the two were the same directory, so comparing a named
// `.` against the working directory was correct. From `modules/tasks` it is not: `cohere .` there
// would have checked all of ahra, turning the narrowest thing a caller can type into the widest run.
func TestDotFromASubdirectoryIsThatSubdirectory(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"Top.ts":           "",
		"modules/Inner.ts": "",
	})
	modules := filepath.Join(root, "modules")

	scope, err := namedPathsScope(modules, root, []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	if scope.Everything {
		t.Fatal("`.` from a subdirectory became the whole project")
	}
	if !scope.includes(filepath.Join(modules, "Inner.ts")) || scope.includes(filepath.Join(root, "Top.ts")) {
		t.Errorf("`.` did not mean the subdirectory: %v", scope.FileNames)
	}

	// The control: `.` at the root still means everything.
	whole, err := namedPathsScope(root, root, []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	if !whole.Everything {
		t.Errorf("`.` at the root stopped meaning the whole tree: %v", whole.FileNames)
	}
}
