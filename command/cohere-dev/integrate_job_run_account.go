package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/command_line/streams"
)

// The integrate job's account (#ged8ew1): plain lines, each opening with integrateJobMarker, which Kirk's Square rows
// from the first landings' logs. Each step's wall seconds and the job's total; the kept Go cache's size and entries
// before and after the tests, which say how much was compiled against how much was cached, read that way rather than
// counted compile by compile; how many test packages ran and how many replayed `(cached)`, for the plain run and the
// -race run; every step the replay store skipped; and the volume's free space and the integrate path's size before and
// after. A line that reads the disk says its own seconds beside it, since `du` of a big cache is what may cost.

// say prints one account line, integrateJobMarker and what follows, to the job's log as it happens.
func (job *integrateJobRun) say(format string, arguments ...any) {
	fmt.Fprintf(streams.Errors, integrateJobMarker+format+"\n", arguments...)
	streams.Show(streams.Errors)
}

// integratePaths are what the integrate job's account measures the size of: its scratch, every tree, mirror and corpus
// laid under it, and its kept cache, when one is set.
func (job *integrateJobRun) integratePaths() []string {
	paths := []string{filepath.Dir(job.scratch)}
	if job.cache != "" {
		paths = append(paths, job.cache)
	}
	return paths
}

// sayDisk says the volume's free space under the integrate path and the integrate path's size, when, `before` or
// `after`, each with the seconds it took to read.
func (job *integrateJobRun) sayDisk(when string) {
	paths := job.integratePaths()
	started := time.Now()
	free, freeError := freeKilobytes(paths[0])
	if freeError != nil {
		job.say("disk %s free unread: %v", when, freeError)
	} else {
		job.say("disk %s free %d KiB (df -k, %s)", when, free, integrateJobSeconds(time.Since(started)))
	}
	started = time.Now()
	total := int64(0)
	for _, path := range paths {
		size, sizeError := sizeKilobytes(path)
		if sizeError != nil {
			job.say("disk %s integrate path unread at %s: %v", when, path, sizeError)
			return
		}
		total += size
	}
	job.say("disk %s integrate path %d KiB, %s (du -sk, %s)", when, total, strings.Join(paths, " and "),
		integrateJobSeconds(time.Since(started)))
}

// cacheReading is the kept Go cache's size and entries at one moment, and what reading them cost.
type cacheReading struct {
	kilobytes int64
	entries   int
	spent     time.Duration
	unread    error
}

// readCache reads the Go cache the tests run on as it is now: the kept one, `gocache` under the integrate job's kept
// directory, or with none kept the runner's declared layer, `GOCACHE` (runnerDeclaredCache). An empty cache, the
// cache not yet made, reads as nothing in it.
func (job *integrateJobRun) readCache() cacheReading {
	// The shared folder is walked by the runner's trim at the job's start, which says its size (sayCache), and never
	// again here: up to 512 GiB walked twice a job would cost more than it says.
	if job.cache == "" && runnerSharedCache() != "" {
		return cacheReading{}
	}
	started := time.Now()
	cache := filepath.Join(job.cache, "gocache")
	if job.cache == "" && runnerDeclaredCache() {
		cache = strings.TrimSpace(os.Getenv("GOCACHE"))
	}
	entries := 0
	walkError := filepath.WalkDir(cache, func(_ string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.Type().IsRegular() {
			entries++
		}
		return nil
	})
	if walkError != nil && !errors.Is(walkError, fs.ErrNotExist) {
		return cacheReading{unread: walkError, spent: time.Since(started)}
	}
	if walkError != nil {
		return cacheReading{spent: time.Since(started)}
	}
	size, sizeError := sizeKilobytes(cache)
	return cacheReading{kilobytes: size, entries: entries, spent: time.Since(started), unread: sizeError}
}

// sayCache says whose build cache the tests ran on, then its size before and after the tests and the entries it
// gained, which say how much was compiled against how much replayed from it, read from the entry count rather than
// counted compile by compile. On the runner's declared cache (#e94a6r4) the size is its layer's, the one part the job
// writes: a get the shared folder serves writes nothing there, so the packages' replays are on the packages line. With
// neither a kept cache nor the runner's, the script's empty per-job cache is made and removed inside its run, so the
// line says so, and never on the runner's: J-k20kexn's fill ran on the runner's cache and said the empty one.
func (job *integrateJobRun) sayCache(before cacheReading, after cacheReading) {
	if shared := runnerSharedCache(); job.cache == "" && shared != "" {
		job.say(
			"build cache the runner's shared Go cache, %s, read and written in place by every integrate job",
			shared,
		)
		job.say("build cache at start %s", sharedCacheSize())
		return
	}
	if job.cache == "" && !runnerDeclaredCache() {
		job.say("build cache an empty per-job cache, made and removed inside the run, so every package compiled " +
			"and nothing is measured of it")
		return
	}
	if job.cache == "" {
		job.say("build cache the runner's declared Go cache, its layer %s and its program %s",
			strings.TrimSpace(os.Getenv("GOCACHE")), strings.TrimSpace(os.Getenv("GOCACHEPROG")))
	} else {
		job.say("build cache the integrate job's kept Go cache, %s", filepath.Join(job.cache, "gocache"))
	}
	for _, reading := range []cacheReading{before, after} {
		if reading.unread != nil {
			job.say("build cache unread: %v", reading.unread)
			return
		}
	}
	job.say("build cache before %d KiB, %d entries (du -sk and a count, %s)", before.kilobytes, before.entries,
		integrateJobSeconds(before.spent))
	job.say("build cache after %d KiB, %d entries (du -sk and a count, %s)", after.kilobytes, after.entries,
		integrateJobSeconds(after.spent))
	job.say("build cache gained %d entries: compiled against cached is read from the entry count, so a run that "+
		"gained none compiled nothing new, never an exact count of compiles", after.entries-before.entries)
}

// sayPackages says how many test packages ran and how many replayed `(cached)`, for the run's plain tests and its
// -race run each, from output, the box script's `go test -json` stream with boxRaceMarker between the two.
func (job *integrateJobRun) sayPackages(output []byte) {
	plain, race, raced := bytes.Cut(output, []byte("\n"+boxRaceMarker+"\n"))
	for _, run := range []struct {
		name   string
		stream []byte
		was    bool
	}{{"tests", plain, true}, {"-race", race, raced}} {
		if !run.was {
			job.say("packages %s none run", run.name)
			continue
		}
		ended, cached := packagesCached(run.stream)
		job.say("packages %s %d ran, %d replayed (cached)", run.name, ended-cached, cached)
	}
}

// packagesCached reads a `go test -json` stream and returns how many packages ended, a pass or a fail, and how many of
// them replayed from the cache, their package output line ending in `(cached)`.
func packagesCached(stream []byte) (int, int) {
	ended, cached := map[string]bool{}, map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(stream))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var event testEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Package == "" || event.Test != "" {
			continue
		}
		switch event.Action {
		case "pass", "fail":
			ended[event.Package] = true
		case "output":
			if strings.HasSuffix(strings.TrimSpace(event.Output), "(cached)") {
				cached[event.Package] = true
			}
		}
	}
	count := 0
	for name := range cached {
		if ended[name] {
			count++
		}
	}
	return len(ended), count
}

// sizeKilobytes is `du -sk` of path.
func sizeKilobytes(path string) (int64, error) {
	output, duError := exec.Command("du", "-sk", path).Output()
	if duError != nil {
		return 0, fmt.Errorf("du -sk %s: %w", path, duError)
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return 0, fmt.Errorf("du -sk %s said nothing", path)
	}
	return strconv.ParseInt(fields[0], 10, 64)
}

// freeKilobytes is the free space, in KiB, `df -k` says of the volume path is on.
func freeKilobytes(path string) (int64, error) {
	output, dfError := exec.Command("df", "-k", path).Output()
	if dfError != nil {
		return 0, fmt.Errorf("df -k %s: %w", path, dfError)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("df -k %s said %q", path, output)
	}
	return strconv.ParseInt(fields[3], 10, 64)
}

// sharedCacheSize is what the runner's trim at the job's start said of the shared Go cache folder: its size as the
// walk found it, its size after the trim and the entries the trim removed, or that it wasn't read, when the walk
// stopped at its bound and the runner set none of them, or set one that is no whole count.
func sharedCacheSize() string {
	figures := []int64{}
	for _, variable := range []string{runnerSharedCacheBytesBeforeVariable, runnerSharedCacheBytesVariable,
		runnerSharedCacheRemovedVariable} {
		value := strings.TrimSpace(os.Getenv(variable))
		figure, parseError := strconv.ParseInt(value, 10, 64)
		if value == "" || parseError != nil || figure < 0 {
			return fmt.Sprintf("not read: the runner's trim set %s=%q", variable, value)
		}
		figures = append(figures, figure)
	}
	return fmt.Sprintf("%d bytes found, %d bytes after the runner's trim, %d entries removed (the runner's walk)",
		figures[0], figures[1], figures[2])
}
