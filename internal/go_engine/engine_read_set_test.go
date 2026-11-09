package go_engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readSetRun is the read-set fixture copied, run cold, then with bottom.Double's body replaced by body, which keeps
// its API and every position: the warm run's counts and its count of SA4017 findings, the count under `--no-cache`
// over the same tree, and the root, which holds the store the warm run left.
func readSetRun(test *testing.T, body string) (warm cachedRun, warmFindings int, uncachedFindings int, root string) {
	test.Helper()
	root = copyFixture(test, filepath.Join("testdata", "read-set"))
	cold := runCachedEngine(test, root)
	if found := strings.Count(cold.stream, `"rule":"cohere-go/SA4017"`); found != 1 {
		test.Fatalf("want SA4017's one finding on the dropped call to the pure bottom.Double cold, got %d", found)
	}
	file := filepath.Join(root, "bottom", "bottom.go")
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatalf("reading %s: %v", file, readError)
	}
	edited := strings.Replace(string(contents), "\treturn value * 2\n", body, 1)
	if edited == string(contents) {
		test.Fatalf("the fixture's Double has no body to replace")
	}
	if writeError := os.WriteFile(file, []byte(edited), 0o644); writeError != nil {
		test.Fatalf("editing %s: %v", file, writeError)
	}
	warm = runCachedEngine(test, root)
	uncached := runCachedEngine(test, root, "--no-cache")
	return warm, strings.Count(warm.stream, `"rule":"cohere-go/SA4017"`),
		strings.Count(uncached.stream, `"rule":"cohere-go/SA4017"`), root
}

// An edit that makes bottom.Double impure behind an unchanged API and unchanged positions leaves top's second key as
// it was, since no output digest holds purity, so only top's read set can say its finding no longer holds: top read
// Double pure, the run now judges it not pure, and top misses and drops the finding, as `--no-cache` does (#w7zmvat
// wave 3b).
func TestAnImporterWhoseReadNoLongerHoldsMisses(test *testing.T) {
	test.Parallel()
	warm, warmFindings, uncachedFindings, _ := readSetRun(test, "\tprintln(value)\n\treturn value * 2\n")
	if warmFindings != 0 || uncachedFindings != 0 {
		test.Fatalf("want no SA4017 finding once Double is impure, warm %d, under --no-cache %d, counts %+v",
			warmFindings, uncachedFindings, warm.counts)
	}
	if warm.counts.Hit != 0 {
		test.Fatalf("want top missed, its read of Double's purity no longer holding, got %+v", warm.counts)
	}
}

// An edit to bottom.Double's body that keeps it pure moves bottom's first key, but the answer top read of it holds, so
// top is still restored by its second key and the early cutoff keeps what it saved. The run after, with nothing
// changed, restores top by the alias the second key left under top's new first key, its read checked against the
// answer bottom's new entry keeps, with no wave at all.
func TestAnImporterWhoseReadsHoldIsRestoredByItsSecondKey(test *testing.T) {
	test.Parallel()
	warm, warmFindings, uncachedFindings, root := readSetRun(test, "\treturn value + value\n")
	if warmFindings != 1 || uncachedFindings != 1 {
		test.Fatalf("want SA4017's finding kept, Double still pure, warm %d, under --no-cache %d", warmFindings,
			uncachedFindings)
	}
	if warm.counts.Hit != 1 || warm.counts.Missed != 1 {
		test.Fatalf("want bottom missed and top restored by its second key, got %+v", warm.counts)
	}
	again := runCachedEngine(test, root)
	if again.counts.Hit != 2 || again.counts.Missed != 0 || again.counts.Waves != 0 ||
		again.withoutCounts != warm.withoutCounts {
		test.Fatalf("want both restored, top by its alias, with no wave and the warm run's stream, got %+v",
			again.counts)
	}
}
