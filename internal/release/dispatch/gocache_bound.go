package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/release/gocache"
)

// goCacheCap is the size the launcher's private Go build cache may reach before it is trimmed.
//
// Go trims its cache only of entries unused for five days, and nothing else bounds it. This one takes a
// build from every commit and every `--dev` run of every member, and on 2026-10-03 it reached 310 GB and
// filled the disk (#bdw0dhn). A cold build of cohere and its compiler fills a few GB, so the cap holds
// several builds' worth. A variable so a test can cross it with a small file.
var goCacheCap int64 = 32 << 30

// defaultGoCacheCap is the size the default Go build cache, the one every plain `go build`, `go test` and
// `go vet` uses, may reach before the launcher trims it (#3sgjy0h).
//
// Developing cohere fills it far faster than Go's five days. On 2026-10-03 the org filled it at about 25 GB
// an hour, 223 GB in nine hours, when one cold `go test ./...` added 29 GB, 15 of them cohere
// cross-compiled by TestEveryReleaseTargetCompiles. Since 3dc6e70 type-checks those targets instead, a
// cold run adds about 11 GB, and a run over a changed tree 9 to 12 GB more, all of it test variants of
// the packages the change reaches. 48 GB holds a cold run and the changes after it. Entries used within
// gocache.InUseWindow are never removed, so a burst can stand above the cap by what it wrote inside the
// window: at 25 GB an hour and a 90-minute window, about 37 GB. A variable so a test can cross it with a
// small file.
var defaultGoCacheCap int64 = 48 << 30

// goCacheCheckInterval is how often a cache is measured at most. Measuring walks every entry, a third of
// a second on a full cache, and runs land seconds apart on a busy day.
const goCacheCheckInterval = 10 * time.Minute

// GoCacheBound is what one look at a Go build cache found and did.
type GoCacheBound struct {
	// Measured is false when the cache was measured within the interval, and nothing else was done.
	Measured bool
	Trimmed  gocache.Trimmed
}

// goCacheCheckedPath is the file whose modification time says when the launcher's cache was last measured.
func (paths Paths) goCacheCheckedPath() string {
	return filepath.Join(paths.CacheDirectory, "gocache.checked")
}

// defaultGoCacheCheckedPath is the same for the default cache.
func (paths Paths) defaultGoCacheCheckedPath() string {
	return filepath.Join(paths.CacheDirectory, "default-gocache.checked")
}

// claimCheck reports whether a check stamped at stampPath is due, and stamps it when it is. The stamp is
// written before anything is measured, so runs landing together measure once between them.
func claimCheck(stampPath string, now time.Time) (bool, error) {
	if information, err := os.Stat(stampPath); err == nil && now.Sub(information.ModTime()) < goCacheCheckInterval {
		return false, nil
	}
	if err := os.WriteFile(stampPath, nil, 0o644); err != nil {
		return false, fmt.Errorf("stamping the Go cache check: %w", err)
	}
	if err := os.Chtimes(stampPath, now, now); err != nil {
		return false, fmt.Errorf("stamping the Go cache check: %w", err)
	}
	return true, nil
}

// BoundGoCache trims the launcher's Go build cache of its least recently used entries once it is larger
// than goCacheCap, at most once per interval, and logs what it did in the prune log. It runs after each
// build, the only time that cache grows.
func BoundGoCache(paths Paths, now time.Time) (GoCacheBound, error) {
	due, err := claimCheck(paths.goCacheCheckedPath(), now)
	if err != nil || !due {
		return GoCacheBound{}, err
	}
	return trimAndLog(paths, paths.GoCacheDirectory(), goCacheCap, now)
}

// ClaimDefaultGoCacheCheck reports whether the default cache is due a look, at most once per interval,
// and claims it. The launcher calls it on every run, where it costs one stat.
func ClaimDefaultGoCacheCheck(paths Paths, now time.Time) (bool, error) {
	return claimCheck(paths.defaultGoCacheCheckedPath(), now)
}

// BoundDefaultGoCache trims the default Go build cache once it is larger than defaultGoCacheCap, and logs
// what it did in the prune log. It always measures; ClaimDefaultGoCacheCheck is what rations it.
func BoundDefaultGoCache(paths Paths, now time.Time) (GoCacheBound, error) {
	directory, err := DefaultGoCacheDirectory(paths)
	if err != nil {
		return GoCacheBound{}, err
	}
	return trimAndLog(paths, directory, defaultGoCacheCap, now)
}

// DefaultGoCacheDirectory asks the go command which cache a plain `go build` in the checkout uses: the
// caller's `GOCACHE` when it is set, Go's own default otherwise.
func DefaultGoCacheDirectory(paths Paths) (string, error) {
	command := exec.Command("go", "env", "GOCACHE")
	command.Dir = paths.ModuleDirectory
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("asking the go command for its build cache: %w", err)
	}
	directory := strings.TrimSpace(string(output))
	if directory == "" || directory == "off" {
		return "", fmt.Errorf("the go command has no build cache here (GOCACHE=%q), so there is nothing to bound", directory)
	}
	return directory, nil
}

// trimAndLog trims one cache to a cap and appends what it removed to the prune log.
func trimAndLog(paths Paths, directory string, capBytes int64, now time.Time) (GoCacheBound, error) {
	bound := GoCacheBound{Measured: true}
	trimmed, err := gocache.Trim(directory, gocache.LimitFor(capBytes), now)
	bound.Trimmed = trimmed
	if err != nil || trimmed.Removed == 0 {
		return bound, err
	}

	log, err := os.OpenFile(paths.PruneLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return bound, fmt.Errorf("opening the prune log: %w", err)
	}
	defer log.Close()
	_, err = fmt.Fprintf(log, "%s trimmed the Go build cache at %s from %d to %d bytes, over the cap of %d: %d entries removed, %d bytes used within %s kept\n",
		now.Format(time.RFC3339), directory, trimmed.Before, trimmed.After, capBytes,
		trimmed.Removed, trimmed.InUse, gocache.InUseWindow)
	return bound, err
}

// boundGoCacheAfterBuild runs BoundGoCache, noting what it trimmed for --verbose and saying why it could
// not. It never fails the build that called it: the binary is already built, and a cache over its cap is
// trimmed by the next build that can.
func boundGoCacheAfterBuild(paths Paths) {
	bound, err := BoundGoCache(paths, time.Now())
	if err != nil {
		Report.Fail("cohere: the Go build cache was not bounded: %v", err)
		return
	}
	if bound.Trimmed.Removed > 0 {
		Report.Note("cohere: trimmed the launcher's Go build cache from %.1f GiB to %.1f GiB, over its %d GiB cap",
			float64(bound.Trimmed.Before)/(1<<30), float64(bound.Trimmed.After)/(1<<30), goCacheCap>>30)
	}
}
