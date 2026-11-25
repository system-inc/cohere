package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/command_line/streams"
)

// The land job's account (#ged8ew1): plain lines, each opening with landJobMarker, which Kirk's Square rows from the
// first landings' logs. Each step's wall seconds and the job's total; the kept Go cache's size and entries before and
// after the tests, which say how much was compiled against how much was cached, read that way rather than counted
// compile by compile; how many test packages ran and how many replayed `(cached)`, for the plain run and the -race run;
// every step the replay store skipped; and the volume's free space and the land path's size before and after. A line
// that reads the disk says its own seconds beside it, since `du` of a big cache is what may cost.

// say prints one account line, landJobMarker and what follows, to the job's log as it happens.
func (job *landJobRun) say(format string, arguments ...any) {
	fmt.Fprintf(streams.Errors, landJobMarker+format+"\n", arguments...)
	streams.Show(streams.Errors)
}

// landPaths are what the land job's account measures the size of: its scratch, every tree, mirror and corpus laid
// under it, and its kept cache, when one is set.
func (job *landJobRun) landPaths() []string {
	paths := []string{filepath.Dir(job.scratch)}
	if job.cache != "" {
		paths = append(paths, job.cache)
	}
	return paths
}

// sayDisk says the volume's free space under the land path and the land path's size, when, `before` or `after`, each
// with the seconds it took to read.
func (job *landJobRun) sayDisk(when string) {
	paths := job.landPaths()
	started := time.Now()
	free, freeError := freeKilobytes(paths[0])
	if freeError != nil {
		job.say("disk %s free unread: %v", when, freeError)
	} else {
		job.say("disk %s free %d KiB (df -k, %s)", when, free, landJobSeconds(time.Since(started)))
	}
	started = time.Now()
	total := int64(0)
	for _, path := range paths {
		size, sizeError := sizeKilobytes(path)
		if sizeError != nil {
			job.say("disk %s land path unread at %s: %v", when, path, sizeError)
			return
		}
		total += size
	}
	job.say("disk %s land path %d KiB, %s (du -sk, %s)", when, total, strings.Join(paths, " and "),
		landJobSeconds(time.Since(started)))
}

// cacheReading is the kept Go cache's size and entries at one moment, and what reading them cost.
type cacheReading struct {
	kilobytes int64
	entries   int
	spent     time.Duration
	unread    error
}

// readCache reads the kept Go cache, `gocache` under the land job's kept directory, as it is now. An empty cache, the
// cache not yet made, reads as nothing in it.
func (job *landJobRun) readCache() cacheReading {
	started := time.Now()
	cache := filepath.Join(job.cache, "gocache")
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

// sayCache says the build cache before and after the tests, and the entries it gained, which say how much was
// compiled against how much replayed from it, read from the entry count rather than counted compile by compile. With
// no kept cache, the script's empty per-job cache is made and removed inside its run, so the line says so.
func (job *landJobRun) sayCache(before cacheReading, after cacheReading) {
	if job.cache == "" {
		job.say("build cache an empty per-job cache, made and removed inside the run, so every package compiled " +
			"and nothing is measured of it")
		return
	}
	for _, reading := range []cacheReading{before, after} {
		if reading.unread != nil {
			job.say("build cache unread: %v", reading.unread)
			return
		}
	}
	job.say("build cache before %d KiB, %d entries (du -sk and a count, %s)", before.kilobytes, before.entries,
		landJobSeconds(before.spent))
	job.say("build cache after %d KiB, %d entries (du -sk and a count, %s)", after.kilobytes, after.entries,
		landJobSeconds(after.spent))
	job.say("build cache gained %d entries: compiled against cached is read from the entry count, so a run that "+
		"gained none compiled nothing new, never an exact count of compiles", after.entries-before.entries)
}

// sayPackages says how many test packages ran and how many replayed `(cached)`, for the run's plain tests and its
// -race run each, from output, the box script's `go test -json` stream with boxRaceMarker between the two.
func (job *landJobRun) sayPackages(output []byte) {
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
