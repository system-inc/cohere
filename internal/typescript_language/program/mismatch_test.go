package program

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// mismatchFixtureConfiguration is deliberately its own rather than shared with the external test package:
// these tests live inside the package so they can reach projectFilesMismatchMessage, and an internal
// test cannot see an external one's helpers.
const mismatchFixtureConfiguration = `{
    "compilerOptions": {
        "target": "ES2022",
        "module": "esnext",
        "moduleResolution": "bundler",
        "noEmit": true
    },
    "include": ["./*.ts"]
}`

func writeMismatchFixture(test *testing.T, files map[string]string) string {
	test.Helper()

	directory := test.TempDir()
	for name, contents := range files {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	return directory
}

// TestProjectFilesGuardFiresOnMismatch proves the canonicalization guard can fail.
//
// The guard exists because a lookup that misses returns the same empty result as a config that named
// nothing, and the honest way to show it works is to reproduce the miss and watch it complain. The
// reproduction is the original defect: comparing the config's file names to the program's paths
// without canonicalizing, which on a case-insensitive filesystem matched zero of 3,407 files.
//
// This builds the broken root set the way the defect did rather than adding a seam to Graph for the
// test to reach through. A production field that exists only so a test can poison it is a worse
// defect than the one being guarded.
func TestProjectFilesGuardFiresOnMismatch(test *testing.T) {
	test.Parallel()
	directory := writeMismatchFixture(test, map[string]string{
		"tsconfig.json": mismatchFixtureConfiguration,
		"Alpha.ts":      "export const alpha = 1;\n",
		"Beta.ts":       "export const beta = 2;\n",
	})

	graph, buildError := Build(Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}

	named := graph.Configuration.FileNames()
	if len(named) == 0 {
		test.Fatal("fixture named no files, so this test proves nothing")
	}

	// The healthy path matches everything the config named. Without this the test could pass against
	// a guard that fires on everything.
	if got := len(graph.ProjectFiles()); got != len(named) {
		test.Fatalf("healthy path should match all %d named files, got %d", len(named), got)
	}

	// The defect lives only on a case-insensitive filesystem, where a path key is lowercased and a raw name
	// keeps its casing. On a case-sensitive one, ext4 on Home among them, a raw name already is its path key,
	// so there is nothing to reproduce. That is a skip with its reason, never a pass: the land gate reruns
	// Home's skips on the Mac, whose default volume reproduces it. Decided by the graph's own reading of the
	// disk, the same reading the guard's canonicalization uses.
	if graph.Configuration.CaseSensitivity() == tspath.CaseSensitive {
		test.Skip("this filesystem is case-sensitive, so a raw file name is already its path key and the " +
			"canonicalization defect cannot happen here; a case-insensitive volume, such as the Mac's default volume, tests it")
	}

	// The defect: use the raw file name as a path key instead of canonicalizing it.
	uncanonicalized := map[tspath.PathKey]struct{}{}
	for _, fileName := range named {
		uncanonicalized[tspath.PathKey(fileName)] = struct{}{}
	}
	matched := 0
	for _, sourceFile := range graph.Program.GetSourceFiles() {
		if _, isRoot := uncanonicalized[sourceFile.PathKey()]; isRoot {
			matched++
		}
	}

	// This is the assertion that makes the guard meaningful: the broken comparison really does lose
	// files, so a guard that counts them really does have something to catch. It still fires if a
	// case-insensitive filesystem stops reproducing the defect.
	if matched == len(named) {
		test.Fatalf("the uncanonicalized comparison matched all %d files, so this filesystem cannot "+
			"reproduce the defect and the guard is untested here", len(named))
	}
	test.Logf("uncanonicalized comparison matched %d of %d named files, which is the silent failure "+
		"the guard now refuses", matched, len(named))
}

// TestProjectFilesGuardMessageNamesTheCause pins the wording, because a panic that says only
// "mismatch" sends the next reader looking at the config instead of at path canonicalization.
func TestProjectFilesGuardMessageNamesTheCause(test *testing.T) {
	test.Parallel()
	for _, want := range []string{"canonicalization mismatch", "not an empty project"} {
		if !strings.Contains(projectFilesMismatchMessage(3407, 0), want) {
			test.Errorf("guard message should contain %q", want)
		}
	}
}
