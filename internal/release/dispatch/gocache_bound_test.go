package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The launcher's Go build cache is emptied with Go's own command once it is over its cap, measured at most
// once per interval, and left alone under it (#bdw0dhn, where it reached 310 GB and filled the disk). The
// cache here is a real directory in Go's layout, cleaned by the real `go clean -cache`, with a cap small
// enough to cross with one entry.
func TestTheGoCacheIsEmptiedOverItsCapAndKeptUnderIt(t *testing.T) {
	previous := goCacheCap
	goCacheCap = 1 << 20
	t.Cleanup(func() { goCacheCap = previous })

	paths := Paths{ModuleDirectory: t.TempDir(), CacheDirectory: t.TempDir()}
	entry := func(name string, bytes int) string {
		t.Helper()
		path := filepath.Join(paths.GoCacheDirectory(), "ab", name)
		writeFile(t, path, strings.Repeat("x", bytes))
		return path
	}

	under := entry("00000000000000000000000000000000000000000000000000000000000000ab-d", 1024)
	now := time.Now()
	bound, err := BoundGoCache(paths, now)
	if err != nil {
		t.Fatal(err)
	}
	if !bound.Measured || bound.Cleaned {
		t.Fatalf("a cache under its cap: %+v", bound)
	}
	if _, err := os.Stat(under); err != nil {
		t.Fatalf("a cache under its cap lost an entry: %v", err)
	}

	over := entry("11111111111111111111111111111111111111111111111111111111111111ab-d", 2<<20)
	if bound, err := BoundGoCache(paths, now.Add(time.Minute)); err != nil || bound.Measured {
		t.Fatalf("measured again within the interval: %+v, %v", bound, err)
	}

	bound, err = BoundGoCache(paths, now.Add(goCacheCheckInterval+time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !bound.Measured || !bound.Cleaned || bound.Bytes <= goCacheCap {
		t.Fatalf("a cache over its cap: %+v", bound)
	}
	for _, path := range []string{under, over} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived the clean: %v", filepath.Base(path), err)
		}
	}
	log, err := os.ReadFile(paths.PruneLogPath())
	if err != nil || !strings.Contains(string(log), "emptied the Go build cache at "+paths.GoCacheDirectory()) {
		t.Fatalf("the prune log does not say the cache was emptied: %q, %v", log, err)
	}
}
