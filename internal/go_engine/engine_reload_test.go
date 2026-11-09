package go_engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A question of a fact computed on demand that reaches a package the cache restored, with no answer stored, is never
// answered no fact: the run loads that package again from source and runs again, and writes what a run without the
// cache writes (#w7zmvat wave 3). Cold, nothing asks the purity of bottom.Double, so bottom's entry holds no answer of
// it. The edit adds Drop to top, which discards bottom.Double's result: top misses, bottom hits, and SA4017 asks
// whether bottom.Double is pure, which only bottom's source can say. The run reloads bottom once and reports the call,
// as `--no-cache` does. The answer is then in bottom's entry, so the next edit to top asks it again and reloads
// nothing.
func TestARestoredPackageWithNoAnswerIsReloadedAndAnswersAsTheColdPath(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "reload-answer"))
	cold := runCachedEngine(test, root)
	if cold.counts.Reloads != 0 || cold.counts.Written == 0 {
		test.Fatalf("want a cold run that wrote its entries and reloaded nothing, got %+v", cold.counts)
	}

	file := filepath.Join(root, "top", "top.go")
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatalf("reading %s: %v", file, readError)
	}
	drop := "\n// Drop calls bottom.Double and drops what it returns.\nfunc Drop() {\n\tbottom.Double(2)\n}\n"
	if writeError := os.WriteFile(file, append(contents, []byte(drop)...), 0o644); writeError != nil {
		test.Fatalf("editing %s: %v", file, writeError)
	}
	edited := runCachedEngine(test, root)
	if !strings.Contains(edited.stream, `"rule":"cohere-go/SA4017"`) {
		test.Fatalf("want SA4017's finding on the dropped call to bottom.Double, got\n%s", edited.stream)
	}
	uncached := runCachedEngine(test, root, "--no-cache")
	if edited.withoutCounts != uncached.withoutCounts {
		test.Fatalf("after the reload\n%s\nunder --no-cache\n%s", edited.withoutCounts, uncached.withoutCounts)
	}
	if edited.counts.Reloads != 1 {
		test.Fatalf("want bottom reloaded once to answer the purity of bottom.Double, got %+v", edited.counts)
	}

	again := append(contents, []byte(drop+"\n// Edited again, so top misses and asks again.\n")...)
	if writeError := os.WriteFile(file, again, 0o644); writeError != nil {
		test.Fatalf("editing %s again: %v", file, writeError)
	}
	stored := runCachedEngine(test, root)
	if stored.counts.Reloads != 0 || stored.counts.Missed != edited.counts.Missed-edited.counts.Reloads {
		test.Fatalf("want bottom's stored answer read with no reload and only top missed, edited %+v, again %+v",
			edited.counts, stored.counts)
	}
	if !strings.Contains(stored.stream, `"rule":"cohere-go/SA4017"`) {
		test.Fatalf("want SA4017's finding from bottom's stored answer, got\n%s", stored.stream)
	}
}
