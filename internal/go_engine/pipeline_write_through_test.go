package go_engine

import (
	"path/filepath"
	"strings"
	"testing"
)

// A cold run with the findings cache has each extra build write through, to its own store, every package it restored
// from the host's run (#b2t9xmf), those its walk left untouched included (#qbvtkfm), so the unchanged run right after
// hits every package of every build in its own store by its first key: the host, hitting its own store, keeps nothing
// to restore from then, and without the write-through each extra build would check every one of those packages from
// source. Its stream is the cold run's but for the counts of the cache and the restore.
func TestTheRunAfterAColdOneHitsEveryBuildsOwnStore(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	cold, coldStream := runOverWith(test, root, exitFindings)
	warm, warmStream := runOverWith(test, root, exitFindings)
	if len(cold.buildRuns) != 3 || len(warm.buildRuns) != 3 {
		test.Fatalf("the runs handed the merge %d and %d runs, want 3 each", len(cold.buildRuns), len(warm.buildRuns))
	}
	restoring := 0
	for _, coldRun := range cold.buildRuns[1:] {
		// A package its walk left untouched is written through as one it restored and judged (#qbvtkfm).
		restored := len(coldRun.result.FromHost.Restored) + len(coldRun.result.FromHost.Untouched)
		if restored > 0 {
			restoring++
		}
		if written := coldRun.result.Cache.WrittenThrough; written != restored {
			test.Errorf("%s's cold run wrote %d entries through of the %d it restored or left untouched, want "+
				"every one", coldRun.build, written, restored)
		}
	}
	if restoring < 2 {
		test.Fatalf("%d extra builds restored from the host's cold run, want at least two", restoring)
	}
	if !strings.Contains(coldStream, `"writtenThrough":`) {
		test.Errorf("the cold run's builds record no count of what they wrote through:\n%s", coldStream)
	}
	for _, warmRun := range warm.buildRuns[1:] {
		counts := warmRun.result.Cache
		if restored := len(warmRun.result.FromHost.Restored) + len(warmRun.result.FromHost.Untouched); restored != 0 ||
			warmRun.checkedFromSource != 0 {
			test.Errorf("%s's warm run restored %d from the host and checked %d from source, want none of either",
				warmRun.build, restored, warmRun.checkedFromSource)
		}
		if counts.Hit == 0 || counts.Missed != 0 || counts.SecondKeyHit != 0 || counts.WrittenThrough != 0 {
			test.Errorf("%s's warm run hit %d, %d of them by the second key, missed %d and wrote %d through, want "+
				"every package hit by its first key", warmRun.build, counts.Hit, counts.SecondKeyHit, counts.Missed,
				counts.WrittenThrough)
		}
	}
	// The warm host hit every package in its store, so the builds' records name bodies its session decoded, and each
	// build takes them from the run's memo rather than read and decode them again (#qqyq61s); the cold host read none.
	shared := 0
	for _, warmRun := range warm.buildRuns[1:] {
		shared += warmRun.result.Cache.SharedBodyHits
	}
	if shared == 0 || !strings.Contains(warmStream, `"sharedBodyHits":`) {
		test.Errorf("the warm run's builds took %d bodies from the run's memo, want some, recorded:\n%s", shared,
			warmStream)
	}
	if strings.Contains(coldStream, `"sharedBodyHits"`) {
		test.Errorf("the cold run's builds record bodies taken from a memo its host left empty:\n%s", coldStream)
	}
	if withoutCache(coldStream) != withoutCache(warmStream) {
		test.Errorf("the warm run's stream differs from the cold run's:\ncold\n%s\nwarm\n%s", coldStream, warmStream)
	}
}

// Under `--no-cache` an extra build still restores from the host's run, and writes nothing through, having no store:
// no build counts a write-through, and no record writes one.
func TestNoBuildWritesThroughWithoutTheCache(test *testing.T) {
	test.Parallel()

	engine, stream := runOver(test, copyFixture(test, filepath.Join("testdata", "other-builds")), exitFindings)
	restored := 0
	for _, run := range engine.buildRuns {
		restored += len(run.result.FromHost.Restored) + len(run.result.FromHost.Untouched)
		if written := run.result.Cache.WrittenThrough; written != 0 {
			test.Errorf("%s wrote %d entries through under --no-cache, want none", run.build, written)
		}
	}
	if restored == 0 {
		test.Fatal("no build restored from the host's run, so nothing tells writing through from not")
	}
	if strings.Contains(stream, `"writtenThrough"`) {
		test.Errorf("a record under --no-cache counts a write-through:\n%s", stream)
	}
	// Nor does any build take a body from the run's memo, which only a store's hits fill (#qqyq61s).
	if strings.Contains(stream, `"sharedBodyHits"`) {
		test.Errorf("a record under --no-cache counts a body taken from the run's memo:\n%s", stream)
	}
}
