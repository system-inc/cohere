package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// goCacheCap is the size the launcher's private Go build cache may reach before it is emptied.
//
// Go trims its cache only of entries unused for five days, and nothing else bounds it. This one takes a
// build from every commit and every `--dev` run of every member, and on 2026-10-03 it reached 310 GB and
// filled the disk (#bdw0dhn). A cold build of cohere and its compiler fills a few GB, so the cap holds
// several builds' worth and costs one cold build each time it is crossed. A variable so a test can cross
// it with a small file.
var goCacheCap int64 = 32 << 30

// goCacheCheckInterval is how often the size is measured at most. Measuring walks every entry, which on
// a full cache is seconds, and builds land minutes apart on a busy day.
const goCacheCheckInterval = 10 * time.Minute

// GoCacheBound is what one look at the Go build cache found and did.
type GoCacheBound struct {
	// Measured is false when the cache was measured within the interval, and nothing else was done.
	Measured bool
	Bytes    int64
	Cleaned  bool
}

// goCacheCheckedPath is the file whose modification time says when the cache was last measured.
func (paths Paths) goCacheCheckedPath() string {
	return filepath.Join(paths.CacheDirectory, "gocache.checked")
}

// BoundGoCache empties the launcher's Go build cache with Go's own `go clean -cache` once it is larger
// than goCacheCap, and logs what it did in the prune log. It runs after each build, the only time the
// cache grows.
//
// Go's own command rather than removing the directory: it knows the cache's layout and leaves a cache Go
// can open again, and it clears this cache alone, since GOCACHE names it. A build running beside it can
// lose an entry it was about to read and fail once; the next run builds again. Emptied rather than trimmed,
// because Go has no command that trims to a size, and a cold build is the whole cost of starting over.
func BoundGoCache(paths Paths, now time.Time) (GoCacheBound, error) {
	if information, err := os.Stat(paths.goCacheCheckedPath()); err == nil && now.Sub(information.ModTime()) < goCacheCheckInterval {
		return GoCacheBound{}, nil
	}
	// Stamped before measuring, so two runs finishing together measure once between them.
	if err := os.WriteFile(paths.goCacheCheckedPath(), nil, 0o644); err != nil {
		return GoCacheBound{}, fmt.Errorf("stamping the Go cache check: %w", err)
	}
	if err := os.Chtimes(paths.goCacheCheckedPath(), now, now); err != nil {
		return GoCacheBound{}, fmt.Errorf("stamping the Go cache check: %w", err)
	}

	bound := GoCacheBound{Measured: true, Bytes: directoryBytes(paths.GoCacheDirectory())}
	if bound.Bytes <= goCacheCap {
		return bound, nil
	}

	clean := exec.Command("go", "clean", "-cache")
	clean.Dir = paths.ModuleDirectory
	clean.Env = append(os.Environ(), "GOCACHE="+paths.GoCacheDirectory())
	output, err := clean.CombinedOutput()
	if err != nil {
		return bound, fmt.Errorf("emptying the Go build cache at %s (%d bytes): %w: %s",
			paths.GoCacheDirectory(), bound.Bytes, err, strings.TrimSpace(string(output)))
	}
	bound.Cleaned = true

	log, err := os.OpenFile(paths.PruneLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return bound, fmt.Errorf("opening the prune log: %w", err)
	}
	defer log.Close()
	_, err = fmt.Fprintf(log, "%s emptied the Go build cache at %s: %d bytes, over the cap of %d\n",
		now.Format(time.RFC3339), paths.GoCacheDirectory(), bound.Bytes, goCacheCap)
	return bound, err
}

// boundGoCacheAfterBuild runs BoundGoCache and says on stderr what it emptied, or why it could not. It
// never fails the build that called it: the binary is already built, and a cache over its cap is cleared
// by the next build that can.
func boundGoCacheAfterBuild(paths Paths) {
	bound, err := BoundGoCache(paths, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohere: the Go build cache was not bounded: %v\n", err)
		return
	}
	if bound.Cleaned {
		fmt.Fprintf(os.Stderr, "cohere: emptied the launcher's Go build cache (%.1f GB, over its %.0f GB cap); the next build starts cold\n",
			float64(bound.Bytes)/1e9, float64(goCacheCap)/(1<<30))
	}
}
