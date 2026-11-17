package go_engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// A standard-library package an extra build restored from the host's table hit is kept by the system cache alone, so
// the build never writes it through to its own store (#syscache). What the build computes of the standard library
// whole goes to its own table, and the build's next run restores it from there or from the host's run: the run after
// a cold one, with library edited so it misses in every build, checks fewer packages from source than the cold run,
// and a third run checks exactly as many as the second, so nothing the build keeps changes from run to run. What it
// still checks from source beside library is the standard library it checks only for the types of a package above it
// restored from the host, where no fact analyzer's action reaches it, whose entry no table takes as whole
// (check.standardLibraryEntryWhole) and no project's store takes at all.
func TestATableHitIsNeverWrittenThroughAndTheBuildKeepsWhatItComputes(test *testing.T) {
	test.Parallel()

	table := localTable(test)
	root := copyFixture(test, standardLibraryFixture)
	other := map[bool]string{true: "freebsd", false: "linux"}[runtime.GOOS == "linux"]
	if writeError := os.WriteFile(filepath.Join(root, "library_"+other+".go"), []byte("package library\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	reading := generatedTableReading(test, table)
	cold := extraBuildRecords(test, runEngineWith(test, root, reading, rules.All(), "--other-builds").stream)
	for _, build := range cold {
		if build.WrittenThrough != 0 {
			test.Errorf("%s wrote %d standard-library packages through, want none", build.Target,
				build.WrittenThrough)
		}
	}
	checked := func(label string) []int {
		test.Helper()
		edited := filepath.Join(root, "library_"+other+".go")
		if writeError := os.WriteFile(edited, []byte("// Package library is edited "+label+".\npackage library\n"),
			0o644); writeError != nil {
			test.Fatal(writeError)
		}
		builds := extraBuildRecords(test, runEngineWith(test, root, reading, rules.All(), "--other-builds").stream)
		if len(builds) != len(cold) {
			test.Fatalf("the %s run ran %d extra builds, want %d", label, len(builds), len(cold))
		}
		counts := make([]int, 0, len(builds))
		for _, build := range builds {
			counts = append(counts, build.CheckedFromSource)
		}
		return counts
	}
	second, third := checked("second"), checked("third")
	for index, build := range cold {
		if second[index] >= build.CheckedFromSource || second[index] == 0 {
			test.Errorf("%s checked %d packages from source cold and %d after, want fewer after, library among them",
				build.Target, build.CheckedFromSource, second[index])
		}
		if third[index] != second[index] {
			test.Errorf("%s checked %d packages from source the second time and %d the third, want as many",
				build.Target, second[index], third[index])
		}
	}
}
