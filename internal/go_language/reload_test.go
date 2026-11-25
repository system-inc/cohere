package go_language

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/rules"
)

// dropCalls is what the edit to the reload-answer fixture's top adds: Drop, which calls bottom.Double, side.Triple and
// os.Getpagesize and discards all three results, so top misses, and SA4017 asks whether each is pure.
const dropCalls = "\n// Drop calls three functions and drops what they return.\nfunc Drop() {\n\tbottom.Double(2)\n" +
	"\tside.Triple(2)\n\tos.Getpagesize()\n}\n"

// appendToTop appends added to the fixture's top/top.go under root.
func appendToTop(test *testing.T, root string, added string) {
	test.Helper()
	file := filepath.Join(root, "top", "top.go")
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatalf("reading %s: %v", file, readError)
	}
	if writeError := os.WriteFile(file, append(contents, []byte(added)...), 0o644); writeError != nil {
		test.Fatalf("editing %s: %v", file, writeError)
	}
}

// A question of a fact computed on demand that reaches a package the cache restored, with no answer stored, is never
// answered no fact: the run loads that package again from source and runs again, and writes what a run without the
// cache writes (#w7zmvat wave 3). Cold, nothing asks the purity of bottom.Double, side.Triple or os.Getpagesize, and
// the cold run writes bottom's and side's entries with only the answers asked, as a build before format 10 wrote
// every entry but the standard library's (check.Caching's AnswersInPart), so neither holds an answer of either. The
// edit adds Drop to top.
//
// So only bottom's and side's source can say. The pass that asks of bottom is handed a placeholder and runs on,
// asking of side too, so one round reloads both. os's entry the cold run wrote whole, as it writes every other, so it
// answers from its entry and reloads nothing. The last pass reports the calls to Double and Triple, and holds
// os.Getpagesize not pure, as `--no-cache` does. Every answer is then in its package's entry, so the next edit to top
// asks them again and reloads nothing.
func TestARestoredPackageWithNoAnswerIsReloadedAndAnswersAsTheColdPath(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "reload-answer"))
	inPart := map[string]bool{"example.com/reloadanswer/bottom": true, "example.com/reloadanswer/side": true}
	// One system cache for every run, as a person's, so each run after the first restores the standard library the
	// first wrote, and the counts below are of the module's packages alone.
	systemCache := standardLibrarySource{systemCacheRoot: test.TempDir()}
	cold := runEngineAnsweringInPart(test, root, systemCache, rules.All(), inPart)
	if cold.counts.Reloads != 0 || cold.counts.Written == 0 {
		test.Fatalf("want a cold run that wrote its entries and reloaded nothing, got %+v", cold.counts)
	}

	appendToTop(test, root, dropCalls)
	edited := runEngineWith(test, root, systemCache, rules.All())
	if strings.Count(edited.stream, `"rule":"cohere-go/SA4017"`) != 2 {
		test.Fatalf("want SA4017's two findings, on the dropped calls to bottom.Double and side.Triple, got\n%s",
			edited.stream)
	}
	uncached := runCachedEngine(test, root, "--no-cache")
	if edited.withoutCounts != uncached.withoutCounts {
		test.Fatalf("after the reload\n%s\nunder --no-cache\n%s", edited.withoutCounts, uncached.withoutCounts)
	}
	if edited.counts.Reloads != 2 || edited.counts.ReloadRounds != 1 {
		test.Fatalf("want bottom and side reloaded in one round, and os answered from its whole entry, got %+v",
			edited.counts)
	}

	appendToTop(test, root, "\n// Edited again, so top misses and asks again.\n")
	stored := runEngineWith(test, root, systemCache, rules.All())
	if stored.counts.Reloads != 0 || stored.counts.Missed != edited.counts.Missed-edited.counts.Reloads {
		test.Fatalf("want the stored answers read with no reload and only top missed, edited %+v, again %+v",
			edited.counts, stored.counts)
	}
	if strings.Count(stored.stream, `"rule":"cohere-go/SA4017"`) != 2 {
		test.Fatalf("want SA4017's findings from the stored answers, got\n%s", stored.stream)
	}
}

// Every entry a run writes holds every answer of its package, computed whole, so a warm edit that asks of a restored
// package what no run asked before reloads nothing (#w7zmvat): the cold run asks nothing of bottom.Double or
// side.Triple, the edit adds Drop to top, which asks the purity of both, and their entries answer, with no round of
// load and lint of their own. The run reports what `--no-cache` reports.
func TestAWarmEditOverWholeEntriesReloadsNothing(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "reload-answer"))
	cold := runCachedEngine(test, root)
	if cold.counts.Reloads != 0 || cold.counts.Written == 0 {
		test.Fatalf("want a cold run that wrote its entries and reloaded nothing, got %+v", cold.counts)
	}

	appendToTop(test, root, dropCalls)
	edited := runCachedEngine(test, root)
	if edited.counts.Reloads != 0 || edited.counts.ReloadRounds != 0 {
		test.Fatalf("want bottom and side answered from their whole entries, nothing reloaded, got %+v",
			edited.counts)
	}
	if strings.Count(edited.stream, `"rule":"cohere-go/SA4017"`) != 2 {
		test.Fatalf("want SA4017's two findings, on the dropped calls to bottom.Double and side.Triple, got\n%s",
			edited.stream)
	}
	uncached := runCachedEngine(test, root, "--no-cache")
	if edited.withoutCounts != uncached.withoutCounts {
		test.Fatalf("from the whole entries\n%s\nunder --no-cache\n%s", edited.withoutCounts, uncached.withoutCounts)
	}
}

// A question of a fact computed on demand whose answer hangs on the order the push walks a package in is answered by
// replaying that walk, which a cold run makes as much as a warm one, so the lint record counts it with the cache and
// without: here SA4023 asks the nilness of Inner's result, and Inner and Outer call each other.
func TestAReplayedAnswerIsCountedWithTheCacheAndWithout(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "replay-answer"))
	for _, extra := range [][]string{nil, {"--no-cache"}} {
		ran := runCachedEngine(test, root, extra...)
		if ran.counts.Replays == 0 || ran.counts.Reloads != 0 {
			test.Fatalf("want the nilness of Inner answered by a replay and nothing reloaded, with %v, got %+v", extra,
				ran.counts)
		}
	}
}

// A standard-library entry in the system cache's store whose answers were computed in part restores as a store hit
// does, and a question it holds no answer to reloads the package from source, as for a project's entry (#syscache):
// a cold run writes os to the system cache with only the answers its questions computed, its first wave saving os
// before any question of os.Getpagesize, and the run after top gains Drop restores os from it, asks the purity of
// os.Getpagesize, reloads os rather than read no answer as no fact, and finds what a run without either cache finds.
func TestAStandardLibraryEntryWithAnswersInPartIsReloaded(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "reload-answer"))
	systemCache := standardLibrarySource{systemCacheRoot: test.TempDir()}
	cold := runEngineAnsweringInPart(test, root, systemCache, rules.All(), map[string]bool{"os": true})
	if cold.counts.SystemCacheWritten == 0 || cold.counts.Reloads != 0 {
		test.Fatalf("want a cold run that wrote the standard library to the system cache and reloaded nothing, got %+v",
			cold.counts)
	}
	appendToTop(test, root, dropCalls)
	edited := runEngineWith(test, root, systemCache, rules.All())
	if edited.counts.FromSystemCache == 0 || edited.counts.Reloads == 0 || edited.counts.ReloadRounds == 0 {
		test.Errorf("want os restored from the system cache, its missing answer reloading it, got %+v", edited.counts)
	}
	plain := runCachedEngine(test, root, "--no-cache", "--no-system-cache")
	if edited.withoutCounts != plain.withoutCounts {
		test.Errorf("after the reload\n%s\nwithout either cache\n%s", edited.withoutCounts, plain.withoutCounts)
	}
}
