package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The replay store (landJobReplay): a pass is recorded under its key and replayed on a matching key without running,
// a key that misses runs, a failure is never recorded, a step with no key always runs and records nothing, and a store
// with no directory always runs.
func TestTheLandJobsReplayStoreReplaysOnlyAPass(test *testing.T) {
	test.Parallel()
	store := landJobReplay{directory: test.TempDir()}
	key := ""
	keyed := func(string) (string, []string, error) { return key, []string{"internal/a.go", "internal/b.go"}, nil }
	runs := 0
	passing := func() (int, string) { runs++; return 0, "the docs data" }
	// A failure that checked something, as a stale docs data check is, so only its exit keeps it unrecorded.
	failing := func() (int, string) { runs++; return 1, "the docs data" }
	for _, row := range []struct {
		name     string
		key      string
		store    landJobReplay
		work     func() (int, string)
		wantRuns int
		wantCode int
		wantSaid string
	}{
		{"a failure runs", "1111", store, failing, 1, 1, "the docs data"},
		{"a failure was never recorded, so it runs again", "1111", store, passing, 2, 0, "the docs data"},
		{"a pass on that key replays", "1111", store, failing, 2, 0,
			"the docs data, replayed from a pass at key 1111"},
		{"a miss runs", "2222", store, passing, 3, 0, "the docs data"},
		{"no key runs", "", store, passing, 4, 0, "the docs data"},
		{"no store runs", "1111", landJobReplay{}, passing, 5, 0, "the docs data"},
		{"a key that is a path runs", "../1111", store, passing, 6, 0, "the docs data"},
	} {
		key = row.key
		code, said := row.store.run("docs-data", keyed, "root", row.work)
		if runs != row.wantRuns || code != row.wantCode || said != row.wantSaid {
			test.Errorf("%s: ran %d times in all, exit %d, said %q; want %d, %d, %q", row.name, runs, code, said,
				row.wantRuns, row.wantCode, row.wantSaid)
		}
	}
	records := listMaybeMissing(test, filepath.Join(store.directory, "steps", "docs-data"))
	names := []string{}
	for _, record := range records {
		names = append(names, record.Name())
	}
	if strings.Join(names, " ") != "1111 2222" {
		test.Errorf("the store holds %q, want the two passes' keys alone", names)
	}
	recorded := string(readMaybeMissing(test, filepath.Join(store.directory, "steps", "docs-data", "1111")))
	if recorded != "the docs data\nrecorded by \ninternal/a.go\ninternal/b.go\n" {
		test.Errorf("the record reads %q, want what was said and the inputs", recorded)
	}
}
