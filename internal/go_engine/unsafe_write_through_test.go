package go_engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// unsafe, the type checker's own package, restored by an extra build from the host's table hit, is written through to
// the build's own store as the build writes it from source, with no export data (#27r8r24): a cold run with the
// findings cache on over a fixture that imports it, with the standard library from the binary's table and a file only
// another target compiles, says nothing on stderr, where the note of an entry not written through would go, counts no
// problem of the cache, and has every extra build write through every package it restored from the host. Without the
// rule, the table, which holds no export data for unsafe, refuses the write, and each build notes it.
func TestUnsafeFromATableHitIsWrittenThrough(test *testing.T) {
	test.Parallel()

	table := localTable(test)
	root := copyFixture(test, standardLibraryFixture)
	// A file of another target than this machine's, so the run has one extra build, and one that imports unsafe
	// itself, so the fixture does not lean on the standard library to reach it.
	other := map[bool]string{true: "freebsd", false: "linux"}[runtime.GOOS == "linux"]
	for name, contents := range map[string]string{
		"library_" + other + ".go": "package library\n",
		"pointer.go": "package library\n\nimport \"unsafe\"\n\n// PointerWidth is how many bytes a pointer " +
			"takes.\nconst PointerWidth = unsafe.Sizeof(uintptr(0))\n",
	} {
		if writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	// runEngineWith fails on anything said on stderr, so a note of the findings cache fails here.
	cold := runEngineWith(test, root, readingTable(table, engineTestsStamp), rules.All(), "--other-builds")
	if !cold.counts.On || cold.counts.FromBinary == 0 {
		test.Fatalf("want a cached run restoring the standard library from the table, got %+v", cold.counts)
	}
	if cold.counts.Problems != 0 {
		test.Errorf("the run counted %d problems of the findings cache, want none", cold.counts.Problems)
	}
	for _, build := range extraBuildRecords(test, cold.stream) {
		if build.RestoredFromHost == 0 {
			test.Fatalf("%s restored nothing from the host, so it tells nothing", build.Target)
		}
		if build.WrittenThrough != build.RestoredFromHost {
			test.Errorf("%s wrote %d entries through of the %d it restored from the host, want every one",
				build.Target, build.WrittenThrough, build.RestoredFromHost)
		}
	}
}
