package go_engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// A standard-library package an extra build restored from the host's table hit and wrote through names its export data
// by digest in the build's own store, as one it checked from source does (#5s1x2hg): the table's entry names none, so
// the entry written through names what the build read. The run after a cold one, with library edited so it misses in
// every build, reads every standard-library package library imports from export data the build's store holds, and
// checks library alone from source in each extra build, where an entry naming no export data would have its package
// checked from source too.
func TestATableHitWrittenThroughNamesItsExportDataByDigest(test *testing.T) {
	test.Parallel()

	table := localTable(test)
	root := copyFixture(test, standardLibraryFixture)
	other := map[bool]string{true: "freebsd", false: "linux"}[runtime.GOOS == "linux"]
	if writeError := os.WriteFile(filepath.Join(root, "library_"+other+".go"), []byte("package library\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	builtIn := readingTable(table, engineTestsStamp)
	cold := runEngineWith(test, root, builtIn, rules.All(), "--other-builds")
	for _, build := range extraBuildRecords(test, cold.stream) {
		if build.WrittenThrough == 0 {
			test.Fatalf("%s wrote nothing through, so it tells nothing", build.Target)
		}
	}
	edited := filepath.Join(root, "library_"+other+".go")
	if writeError := os.WriteFile(edited, []byte("// Package library is edited.\npackage library\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	warm := runEngineWith(test, root, builtIn, rules.All(), "--other-builds")
	builds := extraBuildRecords(test, warm.stream)
	if len(builds) == 0 {
		test.Fatal("the run after the edit ran no extra build")
	}
	for _, build := range builds {
		// Without the digest, each package library imports would be checked from source: 67 on linux.
		if build.CheckedFromSource != 1 || len(build.CheckedFromSourceRounds) > 0 {
			test.Errorf("%s checked %d packages from source over the rounds %v, want library alone", build.Target,
				build.CheckedFromSource, build.CheckedFromSourceRounds)
		}
	}
}
