package go_engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A question of a fact computed on demand that reaches a package the cache restored, with no answer stored, is never
// answered no fact: the run loads that package again from source and runs again, and writes what a run without the
// cache writes (#w7zmvat wave 3). Cold, nothing asks the purity of bottom.Double or os.Getpagesize, so no entry holds
// an answer of either. The edit adds Drop to top, which discards both results: top misses, and SA4017 asks whether
// each is pure, which only its package's source can say. A pass that asks ends at its first unanswered question, so
// the run reloads bottom, then os, then syscall, whose Getpagesize os's source asks of, a pass for each, and keeps os
// from source though its second key holds an entry by then. The last pass reports the call to Double, and holds
// os.Getpagesize not pure, as `--no-cache` does. Every answer is then in its package's entry, so the next edit to top
// asks them again and reloads nothing.
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
	drop := "\n// Drop calls bottom.Double and os.Getpagesize and drops what they return.\nfunc Drop() {\n\tbottom.Double(2)\n\tos.Getpagesize()\n}\n"
	if writeError := os.WriteFile(file, append(contents, []byte(drop)...), 0o644); writeError != nil {
		test.Fatalf("editing %s: %v", file, writeError)
	}
	edited := runCachedEngine(test, root)
	if strings.Count(edited.stream, `"rule":"cohere-go/SA4017"`) != 1 {
		test.Fatalf("want SA4017's one finding, on the dropped call to bottom.Double, got\n%s", edited.stream)
	}
	uncached := runCachedEngine(test, root, "--no-cache")
	if edited.withoutCounts != uncached.withoutCounts {
		test.Fatalf("after the reload\n%s\nunder --no-cache\n%s", edited.withoutCounts, uncached.withoutCounts)
	}
	if edited.counts.Reloads != 3 || edited.counts.ReloadRounds != 3 {
		test.Fatalf("want bottom, os and syscall reloaded, a round each, to answer the purity of bottom.Double and "+
			"os.Getpagesize, got %+v", edited.counts)
	}

	again := append(contents, []byte(drop+"\n// Edited again, so top misses and asks again.\n")...)
	if writeError := os.WriteFile(file, again, 0o644); writeError != nil {
		test.Fatalf("editing %s again: %v", file, writeError)
	}
	stored := runCachedEngine(test, root)
	if stored.counts.Reloads != 0 || stored.counts.Missed != edited.counts.Missed-edited.counts.Reloads {
		test.Fatalf("want the stored answers read with no reload and only top missed, edited %+v, again %+v",
			edited.counts, stored.counts)
	}
	if strings.Count(stored.stream, `"rule":"cohere-go/SA4017"`) != 1 {
		test.Fatalf("want SA4017's finding from the stored answers, got\n%s", stored.stream)
	}
}
