package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The launcher's Go build cache is trimmed of its least recently used entries once it is over its cap,
// measured at most once per interval, and left alone under it (#bdw0dhn, where it reached 310 GB and filled
// the disk). The cache here is a directory in Go's layout, README included, with a cap small enough to
// cross with one entry.
func TestTheGoCacheIsTrimmedOverItsCapAndKeptUnderIt(t *testing.T) {
	previous := goCacheCap
	goCacheCap = 1 << 20
	t.Cleanup(func() { goCacheCap = previous })

	paths := Paths{ModuleDirectory: t.TempDir(), CacheDirectory: t.TempDir()}
	writeFile(t, filepath.Join(paths.GoCacheDirectory(), "README"),
		"This directory holds cached build artifacts from the Go build system.\n")
	now := time.Now()
	entry := func(name string, bytes int, used time.Time) string {
		t.Helper()
		path := filepath.Join(paths.GoCacheDirectory(), "ab", name)
		writeFile(t, path, strings.Repeat("x", bytes))
		if err := os.Chtimes(path, used, used); err != nil {
			t.Fatal(err)
		}
		return path
	}

	recent := entry("00000000000000000000000000000000000000000000000000000000000000ab-d", 1024, now)
	bound, err := BoundGoCache(paths, now)
	if err != nil {
		t.Fatal(err)
	}
	if !bound.Measured || bound.Trimmed.Removed != 0 {
		t.Fatalf("a cache under its cap: %+v", bound)
	}

	old := entry("11111111111111111111111111111111111111111111111111111111111111ab-d", 2<<20, now.Add(-24*time.Hour))
	if bound, err := BoundGoCache(paths, now.Add(time.Minute)); err != nil || bound.Measured {
		t.Fatalf("measured again within the interval: %+v, %v", bound, err)
	}

	bound, err = BoundGoCache(paths, now.Add(goCacheCheckInterval+time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !bound.Measured || bound.Trimmed.Removed != 1 || bound.Trimmed.After > goCacheCap {
		t.Fatalf("a cache over its cap: %+v", bound)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("the entry unused for a day survived the trim: %v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("the entry used just now was trimmed: %v", err)
	}
	log, err := os.ReadFile(paths.PruneLogPath())
	if err != nil || !strings.Contains(string(log), "trimmed the Go build cache at "+paths.GoCacheDirectory()) {
		t.Fatalf("the prune log does not say the cache was trimmed: %q, %v", log, err)
	}
}

// The default cache is the one the caller's own `go` commands use, as the go command names it, and it is
// trimmed to its own cap and claimed at most once per interval (#3sgjy0h). Positive control: an entry over
// the cap and unused for a day is removed, from the directory `GOCACHE` names.
func TestTheDefaultGoCacheIsTheCallersAndIsTrimmed(t *testing.T) {
	previous := defaultGoCacheCap
	defaultGoCacheCap = 1 << 20
	t.Cleanup(func() { defaultGoCacheCap = previous })

	cache := t.TempDir()
	t.Setenv("GOCACHE", cache)
	writeFile(t, filepath.Join(cache, "README"), "This directory holds cached build artifacts from the Go build system.\n")
	paths := Paths{ModuleDirectory: t.TempDir(), CacheDirectory: t.TempDir()}

	directory, err := DefaultGoCacheDirectory(paths)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, _ := filepath.EvalSymlinks(directory); resolved != mustEvalSymlinks(t, cache) {
		t.Fatalf("the default cache is %s, want the GOCACHE the caller set, %s", directory, cache)
	}

	now := time.Now()
	if due, err := ClaimDefaultGoCacheCheck(paths, now); err != nil || !due {
		t.Fatalf("the first look was not due: %v, %v", due, err)
	}
	if due, err := ClaimDefaultGoCacheCheck(paths, now.Add(time.Minute)); err != nil || due {
		t.Fatalf("a second look within the interval was due: %v, %v", due, err)
	}
	if due, err := ClaimDefaultGoCacheCheck(paths, now.Add(goCacheCheckInterval+time.Minute)); err != nil || !due {
		t.Fatalf("a look after the interval was not due: %v, %v", due, err)
	}

	old := filepath.Join(cache, "cd", "22222222222222222222222222222222222222222222222222222222222222cd-d")
	writeFile(t, old, strings.Repeat("x", 2<<20))
	day := now.Add(-24 * time.Hour)
	if err := os.Chtimes(old, day, day); err != nil {
		t.Fatal(err)
	}
	bound, err := BoundDefaultGoCache(paths, now)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Trimmed.Removed != 1 {
		t.Fatalf("the default cache over its cap: %+v", bound)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("the old entry survived: %v", err)
	}
	log, err := os.ReadFile(paths.PruneLogPath())
	if err != nil || !strings.Contains(string(log), "trimmed the Go build cache at "+directory) {
		t.Fatalf("the prune log does not name the default cache: %q, %v", log, err)
	}
}

func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
