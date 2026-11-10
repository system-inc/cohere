package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/system-inc/cohere/go_cache"
)

// The Go build cache holds itself under a cap (#jc6ca7r).
//
// Go trims its cache only of entries unused for five days. The house writes far faster than that: on
// 2026-10-05 the cache went from cleared at 09:01 to 200 GB by 11:30, and had been 347 GB with the disk at
// 99% before. The launcher's trim, since removed (#cpxc2d7), ran every few minutes all morning and
// removed almost nothing, because it spares every entry used in the last 90 minutes, and nearly all of a
// cache that grows by a hundred gigabytes an hour was used in the last 90 minutes. It has to spare them:
// Go reads an entry some time after finding it, and refreshes an entry's time at most once an hour, so a
// trim beside running builds cannot tell an entry a build is reading from one nobody wants. Removing one a
// build was reading failed it with "could not import ... no such file" (#3sgjy0h).
//
// When a token's run ends, at most once every ten minutes for the machine, cohere-dev starts a trim in the
// background. It measures the cache, and only when it is over the cap does it take one token, in arrival order
// like any heavy job, so it doesn't fight the gates for cores, and trim the least recently used entries down to
// three quarters of the cap through go_cache, sparing the last cacheTrimSpare. Then it gives the token back.
//
// It used to wait for an empty pool and take every token, to keep every build that goes through cohere-dev out
// while it removed entries. On a busy day the pool was never empty for five minutes, so the cap never trimmed:
// 82.6 GB over a 64 GB cap on 2026-11-09, the disk losing about 30 GB an hour (#fdxencw). go_cache's floor made
// the hold unnecessary: it removes no entry used within MinimumSpare, which protects any build inside a lock or
// outside it, and Remove looks at each entry again before removing it, leaving one whose time moved. A quiet
// window, opened with `cohere-dev quiet on`, switches the trim off while it holds (quiet.go).

// cacheCapVariable sets the Go build cache's cap, in gigabytes, a fraction allowed.
const cacheCapVariable = "COHERE_DEV_CACHE_GB"

// defaultCacheGigabytes is the cap, set where a trim at cacheTrimSpare can reach it. Under the house's load
// about 28 GB of the cache is used each hour (27.9 and 28.9 GB used within the hour, listed on 2026-11-08 at
// 15:31 and 15:44), so the two hours the trim spares hold about 56 GB it cannot remove, and a cap under that
// is one no trim brings the cache back to. A cold whole-module run writes about 11 GB, and a run after a
// change about 10 more.
const defaultCacheGigabytes = 64

// cacheTargetQuartersOfCap is the target a trim brings the cache down to, in quarters of the cap: 48 GB at
// the default cap, so a cache growing steadily is trimmed once in a while rather than on every look.
const cacheTargetQuartersOfCap = 3

// cacheTrimInterval is how often the machine's cache is looked at, at most.
const cacheTrimInterval = 10 * time.Minute

// cacheTrimSpare is how recently used an entry may be and never be trimmed: go_cache's floor, two hours, Go's
// one-hour lag in marking an entry used and an hour of margin. Any build, through the pool or not (a plain go
// test, gopls, a Loom build on this cache), may be reading an entry it used a moment ago whose time Go left
// almost an hour old, and the floor keeps every entry any build used within the last hour. Removing one such a
// build still needs fails it with a missing file, which costs more than the disk, so the cap is set above what
// the spare keeps (defaultCacheGigabytes).
const cacheTrimSpare = go_cache.MinimumSpare

// trimCacheVerb runs cohere-dev as the background trim. It is what startCacheTrim starts.
const trimCacheVerb = "trim-cache"

func cacheCapBytes() (int64, error) {
	value := os.Getenv(cacheCapVariable)
	if value == "" {
		return defaultCacheGigabytes << 30, nil
	}
	parsed, parseError := strconv.ParseFloat(value, 64)
	if parseError != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s is %q, want a number of gigabytes above 0", cacheCapVariable, value)
	}
	return int64(parsed * (1 << 30)), nil
}

// startCacheTrim starts the background trim when the machine's cache is due a look, and does not wait for
// it. A run pays a stat; the trim is another process, its output nowhere, so no run waits on it.
func startCacheTrim(poolDirectory string) {
	if !filesLock || !claimCacheLook(poolDirectory, time.Now()) {
		return
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		return
	}
	trim := exec.Command(executable, trimCacheVerb)
	trim.Env = outsideAToken(os.Environ())
	if startError := trim.Start(); startError != nil {
		return
	}
	// Ignored: the trim runs on its own whether or not this process lets go of its handle, and on Unix the
	// release frees nothing else.
	_ = trim.Process.Release()
}

// claimCacheLook reports whether the cache is due a look, and claims it, so runs ending together start one
// trim between them.
func claimCacheLook(poolDirectory string, now time.Time) bool {
	stamp := filepath.Join(poolDirectory, "gocache.checked")
	if information, statError := os.Stat(
		stamp,
	); statError == nil && now.Sub(information.ModTime()) < cacheTrimInterval {
		return false
	}
	if os.WriteFile(stamp, nil, 0o644) != nil || os.Chtimes(stamp, now, now) != nil {
		return false
	}
	return true
}

// outsideAToken is an environment with no token held and Go unbudgeted.
func outsideAToken(environment []string) []string {
	kept := make([]string, 0, len(environment))
	for _, variable := range environment {
		if !strings.HasPrefix(variable, heldTokenVariable+"=") && !strings.HasPrefix(variable, "GOMAXPROCS=") {
			kept = append(kept, variable)
		}
	}
	return kept
}

// trimCache is the background trim's whole run: measure, and when over the cap, take a token and trim.
func trimCache() int {
	directory, poolDirectoryError := poolDirectory()
	if poolDirectoryError != nil {
		return 1
	}
	capBytes, cacheCapBytesError := cacheCapBytes()
	if cacheCapBytesError != nil {
		recordCacheLook(directory, cacheCapBytesError.Error())
		return 2
	}
	// One trim at a time, however many were started.
	lock, openError := os.OpenFile(filepath.Join(directory, "gocache-trim.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if openError != nil {
		return 1
	}
	// Ignored: the trim lock is never written, and its lock goes with the descriptor.
	defer lock.Close()
	if taken, tryLockFileError := tryLockFile(lock); tryLockFileError != nil || !taken {
		return 0
	}
	defer unlockFile(lock)

	// A quiet window holds off even the measuring, which walks every entry (quiet.go).
	holds, quietLine := quietHolds(directory, time.Now())
	if holds {
		recordCacheLook(directory, "not looked at: "+quietLine)
		return 0
	}
	if quietLine != "" {
		recordCacheLook(directory, quietLine)
	}

	cache, goCacheDirectoryError := goCacheDirectory()
	if goCacheDirectoryError != nil {
		recordCacheLook(directory, goCacheDirectoryError.Error())
		return 1
	}
	if poolGuardsError := poolGuards(cache); poolGuardsError != nil {
		recordCacheLook(directory, poolGuardsError.Error())
		return 2
	}
	// Measured first, removing nothing, so a cache under its cap costs no one a token.
	listed, listError := go_cache.List(cache)
	if listError != nil {
		recordCacheLook(directory, listError.Error())
		return 1
	}
	measured := go_cache.Total(listed)
	if measured <= capBytes {
		recordCacheLook(directory, fmt.Sprintf("%s under the cap, nothing trimmed", gigabytes(measured)))
		return 0
	}

	shape, readPoolShapeError := readPoolShape()
	if readPoolShapeError != nil {
		recordCacheLook(directory, readPoolShapeError.Error())
		return 2
	}
	waitingSince := time.Now()
	// Its output goes nowhere (startCacheTrim), so nothing is said while it waits; its holder line says why.
	held, waitInLineError := waitInLine(
		directory,
		shape.tokens,
		fmt.Sprintf("trimming the Go build cache, %s over the %s cap", gigabytes(measured), gigabytes(capBytes)),
		"token",
		func() { /* The trim's output goes nowhere, so there is no one to explain the wait to. */ },
	)
	if waitInLineError != nil {
		recordCacheLook(directory, "taking a token to trim: "+waitInLineError.Error())
		return 1
	}
	waited := time.Since(waitingSince)
	// A window opened while the trim waited for its token holds too: the wait can be minutes.
	if holds, quietLine := quietHolds(directory, time.Now()); holds {
		held.release()
		recordCacheLook(directory, fmt.Sprintf(
			"%s over the %s cap, not trimmed: %s",
			gigabytes(measured),
			gigabytes(capBytes),
			quietLine,
		))
		return 0
	}
	started := time.Now()
	trimmed, trimError := go_cache.Trim(
		cache,
		go_cache.Limit{Cap: capBytes, Target: capBytes / 4 * cacheTargetQuartersOfCap, Spare: cacheTrimSpare},
		started,
	)
	held.release()
	if trimError != nil {
		recordCacheLook(directory, trimError.Error())
		return 1
	}
	recordCacheLook(
		directory,
		fmt.Sprintf(
			"trimmed from %s to %s, %d entries removed, %s used in the last %s kept; "+
				"a token was held %s, after waiting %s for it",
			gigabytes(trimmed.Before),
			gigabytes(trimmed.After),
			trimmed.Removed,
			gigabytes(trimmed.InUse),
			cacheTrimSpare,
			time.Since(started).Round(time.Millisecond),
			waited.Round(time.Millisecond),
		),
	)
	return 0
}

// poolGuards refuses a cache outside the user cache directory the pool lives in, the cache this pool's runs draw
// from: Go puts its default cache in the same user cache directory. A run with `HOME` pointed at a scratch
// directory and the live `GOCACHE` passed through held a scratch pool, empty, at once, and trimmed the live cache
// from 112.9 GB to 30 GB under every real gate, its log in the scratch home (2026-10-06 04:44, #d0x1fhp). The pool
// no longer follows `HOME` (poolHome), and this is the guard behind that.
func poolGuards(cache string) error {
	home, poolHomeError := poolHome()
	if poolHomeError != nil {
		return fmt.Errorf("not trimmed: %w", poolHomeError)
	}
	root := userCacheDirectoryIn(home)
	resolvedRoot, rootError := filepath.EvalSymlinks(root)
	resolvedCache, cacheError := filepath.EvalSymlinks(cache)
	if rootError == nil && cacheError == nil {
		if relative, relativePathError := filepath.Rel(
			resolvedRoot,
			resolvedCache,
		); relativePathError == nil && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf(
		"not trimmed: the Go build cache %s is outside %s, the user cache directory this pool lives in, "+
			"so it is not this pool's cache to trim; trim it from a run whose home holds it",
		cache,
		root,
	)
}

// goCacheDirectory is the cache a plain go command here uses: `GOCACHE` when it is set, Go's default
// otherwise.
func goCacheDirectory() (string, error) {
	output, outputError := exec.Command("go", "env", "GOCACHE").Output()
	if outputError != nil {
		return "", fmt.Errorf("asking go for its build cache: %w", outputError)
	}
	directory := strings.TrimSpace(string(output))
	if directory == "" || directory == "off" {
		return "", fmt.Errorf("go has no build cache here (GOCACHE=%q)", directory)
	}
	return directory, nil
}

// recordCacheLook keeps the last look at the cache, for cohere-dev status, and appends it to the trim log.
//
// Its failures go unsaid because there is no one to say them to: the trim is started with its output nowhere
// and its exit code unread (startCacheTrim). The status and the log each hold the look, so either one landing
// keeps it, and a status that never moved into place leaves the last look's.
func recordCacheLook(directory string, what string) {
	line := time.Now().Format("2006-01-02 15:04:05") + " " + what
	temporary := filepath.Join(directory, ".gocache.status")
	if os.WriteFile(temporary, []byte(line+"\n"), 0o644) == nil {
		_ = os.Rename(temporary, filepath.Join(directory, "gocache.status")) // Ignored: no one to tell, as above.
	}
	if log, openError := os.OpenFile(
		filepath.Join(directory, "gocache-trim.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		0o644,
	); openError == nil {
		_, _ = fmt.Fprintln(log, line) // Ignored: no one to tell, as above.
		_ = log.Close()                // Ignored: no one to tell, as above.
	}
}

// cacheStatus is the cache's line in cohere-dev status.
func cacheStatus(directory string) string {
	capBytes, cacheCapBytesError := cacheCapBytes()
	if cacheCapBytesError != nil {
		return "Go build cache: " + cacheCapBytesError.Error()
	}
	last := "never looked at"
	if contents, readError := os.ReadFile(filepath.Join(directory, "gocache.status")); readError == nil {
		last = "last look " + strings.TrimSpace(string(contents))
	}
	return fmt.Sprintf(
		"Go build cache: cap %s, looked at every %s at most; %s",
		gigabytes(capBytes),
		cacheTrimInterval,
		last,
	)
}

func gigabytes(bytes int64) string {
	return strconv.FormatFloat(float64(bytes)/(1<<30), 'f', 1, 64) + " GB"
}
