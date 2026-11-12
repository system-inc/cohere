package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/gitignore"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/internal/types/program"
)

// The run cache's command half: decide whether this run can be replayed, replay it if so, and
// otherwise record it on the way out.
//
// The package half (internal/types/program/run_cache.go) proves inputs unchanged. This half decides
// which runs are eligible and which exits are verdicts, and it is deliberately narrow on both.
//
// # Which runs
//
// A bare `cohere`, `cohere --no-fix`, and `cohere --no-fix --format`, and nothing else. Those are the runs
// people repeat, and every other flag widens the set of things the output could depend on. The key covers
// arguments anyway, so admitting more later is a change to one condition rather than to the proof, plus
// declaring whatever the new run reads that the graph build does not.
//
// At a Go root the same runs are admitted, and the Go engine declares what they read (see go_run_cache.go).
//
// # Which exits
//
// A run is recorded only when it ends in a verdict about the tree: clean, findings, or the type bail.
// A run that fails as a tool (an error out of run) is never recorded, since replaying it would replay a
// failure of the tool as if it were a fact about the code. And a run is recorded only if it declared
// its inputs, so any path that never reaches the declaration (Swift, listings, rename, stdin) cannot be
// recorded by construction rather than by a list someone has to keep complete.
//
// # What a replay prints
//
// The recorded report, framed. That report carries the recorded run's durations, and printed bare
// they would claim this run took seconds and built a graph it did not build. So a replay says when the
// report is from before it, and what this run actually cost after it.

// activeRunCache is the recording in progress, nil when this run is not being recorded.
var activeRunCache *runCacheSession

// runCacheClock is when the run began reading its inputs, the time a recording's inputs must not have changed
// since. Set by startRunCacheClock.
var runCacheClock time.Time

// runCacheClockUnstamped is why runCacheClock is the process's clock rather than a filesystem stamp, empty when it
// is a stamp. A run whose clock is not a stamp is never recorded (see runCacheSession.record).
var runCacheClockUnstamped string

// pendingTypes is the types section of a run the run cache does not record, nil for every other run.
var pendingTypes *typesRecord

// invocationTag marks a line that is true of this invocation and false of any replay of it.
//
// A recorded report says "graph built in 436ms" and "types ran in 409ms". Replayed, those lines claim
// a graph was built that was not, and durations this run did not spend. So each line describing the
// invocation rather than the tree is printed through printInvocation, which tags it; the tee strips
// the tag before the terminal and keeps the line out of what is replayed. The tag is a record
// separator, which no line of cohere's output contains.
var invocationTag = []byte("\x1ecohere-invocation\x1e")

// provenanceTag marks a line that is true of the tree but was produced by a phase, so a replay prints it
// with where it came from.
//
// "fix: 0 of 5 files rewritten, 5 not formatted" is true of an unchanged tree and its not-formatted count
// is actionable, so a replay keeps it. Printed bare it would claim the fix phase ran on a run where
// nothing ran, so it is replayed as "fix (from the cached run at 03:41): ...". Unlike invocationTag the
// line stays in the recording, tagged, so the replay can find it without matching on its text.
var provenanceTag = []byte("\x1ecohere-provenance\x1e")

// recordRunSummary keeps the run's summary for the record, when this run is being recorded.
func recordRunSummary(summary runSummary) {
	if activeRunCache == nil {
		return
	}
	if encoded, encodeError := json.Marshal(summary); encodeError == nil {
		activeRunCache.summary = encoded
	}
}

// runCacheEligible reports whether this invocation may use the run cache at all.
//
// A bare run and `--no-fix`, each with `--format` or `--no-format` or neither, in any order. `--no-fix`
// runs every phase and writes nothing, so it is a pure report, which suits a cache better than the bare
// run does; it is what a real-tree measurement uses, so the probe asks to write nothing as well as being
// sandboxed. Both format by default (#b1sjy7b), so `--format` names what they already do, and their walk
// declares what it read (see declareFormatWalk); `--no-format` leaves formatting out. A bare run that
// rewrites a file declines its own record (see declineRunCache). The key covers the arguments, so none of
// them replays another.
//
// `--verbose`, `--json` and `--phases` choose how the run is printed and change nothing it computes, so
// each of them may join any of those runs. Each is still its own record, because each prints a different
// body: the key covers the arguments, and whether the output is colored.
//
// `--adamic-readiness` measures what the run without it does not, so it is its own record too, and a run
// with it never replays one without it, nor one without it a run with it (#9tgm3dq).
//
// `--other-builds` checks a Go project's other builds too, which a run without it does not, so it is its own record
// as well. Its verdict is replayed only while every build's inputs are unchanged, since the Go engine's inputs record
// names what every build's load read (#e4b08k7), and the land gate runs with it (#72n9mq0).
//
// `--timing` is never admitted, on any engine, so such a run neither replays nor is recorded. A timing run is a
// measurement, and a replay would print the last run's CPU as this run's, a table of costs this run never spent.
//
// `--no-cache` is refused by name rather than left to the argument shape. Most flags make a run
// ineligible, but the promise that flag makes, nothing read and nothing written, should not rest on which
// arguments happen to be admitted next.
//
// One of discovery's children is judged by the command line its parent was given (see runCacheArguments).
func runCacheEligible() bool {
	if cacheOff {
		return false
	}
	return runCacheAdmits(runCacheArguments(os.Args[1:], os.Getenv(projectEngineVariable) != ""))
}

// runCacheArguments is the part of a command line the run cache judges. For one of discovery's children (child, its
// engine variable set) that is what its parent was given: the leading `--directory <project>` and `--tsconfig
// <file>` runProject prepends and the trailing `--lint-config <absolute path>` childArguments appends are taken off,
// in exactly those places and nowhere else. Without that, every project of a root holding several, cohere's own
// among them, ran cold every time, since `--directory` admits no run.
//
// Sound because the key already holds what those flags choose: every argument as given, the project's root
// resolved, its tsconfig and lint config, what it yields to nearer projects, and its label (see beginRunCache and
// beginRunCacheSession). A person who types `--directory` is no child, so their run stays ineligible and the runs
// that replay are still the ones runCacheEligible names, run from a root or as one of its projects. The `--no-cache`
// a parent adds for a shared directory makes its child ineligible through cacheOff.
func runCacheArguments(arguments []string, child bool) []string {
	if !child {
		return arguments
	}
	judged := arguments
	if len(judged) >= 2 && judged[0] == "--directory" {
		judged = judged[2:]
		if len(judged) >= 2 && judged[0] == "--tsconfig" {
			judged = judged[2:]
		}
	}
	if last := len(judged) - 2; last >= 0 && judged[last] == "--lint-config" && filepath.IsAbs(judged[last+1]) {
		judged = judged[:last]
	}
	return judged
}

// runCacheAdmits reports whether arguments are a command line the run cache serves. See runCacheEligible.
func runCacheAdmits(arguments []string) bool {
	seen := map[string]bool{}
	for _, argument := range arguments {
		switch argument {
		case "--no-fix", "--format", "--no-format", "--verbose", "--json", "--phases", "--adamic-readiness",
			"--other-builds":
		default:
			return false
		}
		if seen[argument] {
			return false
		}
		seen[argument] = true
	}
	// Refused by the command line before anything runs, and never a run to record.
	return !(seen["--format"] && seen["--no-format"]) && !(seen["--verbose"] && seen["--json"])
}

// beginRunCache replays a recorded run and exits if every input is unchanged, and otherwise starts
// recording this one. It returns the recorder the graph build should report its inputs to, or nil
// when this run is not eligible.
func beginRunCache(location projectLocation) *program.InputRecorder {
	if !runCacheEligible() {
		return nil
	}
	// No format scope is in the key. Without a formatter (`--no-format`) the scope is a constant. With one,
	// the scope is drawn from a walk that declares every file and directory it read, so a change that would
	// move the scope moves an input instead (see declareFormatWalk). A run that rewrites a file declines its
	// record. The printers need no fact of their own: the key covers the running binary, so any other build
	// of the formatter misses. Whether the output is colored is one, since a body recorded for a terminal
	// carries escape codes that a replay into a pipe must not print.
	// The root resolved, and the files below it from it, so a run through another spelling of the same
	// directory has the same key (#547dhjz).
	anchor := program.NewPathAnchor(location.Root)
	walkSections := program.CacheTableSections{Findings: true, Signatures: true, Types: true}
	session := beginRunCacheSession(location.Root, anchor, walkSections,
		"root="+anchor.Resolved(),
		"tsconfig="+anchor.Stable(location.ConfigFileName),
		"lint-config="+anchor.Stable(location.LintConfigFileName),
		fmt.Sprintf("color=%t", activeOutput.Style.color),
		// What the run leaves to nearer projects changes what it reports, and it arrives by a variable the
		// arguments do not show. See ownership.go.
		"yield="+activeProjectYieldFingerprint(),
	)
	if session == nil {
		return nil
	}
	return session.recorder
}

// beginRunCacheSession is the half of beginning a run cache every engine shares: the key, from this invocation's
// arguments and the facts its engine names; the recorded run's check, which replays it and exits on a hit; and
// otherwise the session that records this run, its streams teed. sections are the parts of the table beyond the
// run that this engine's walk reuses, read only on a miss, since a replay needs nothing else. Nil when no session
// could begin, and then nothing is recorded.
func beginRunCacheSession(
	root string,
	anchor program.PathAnchor,
	sections program.CacheTableSections,
	facts ...string,
) *runCacheSession {
	// The label a project of several is run under, which its summary carries and so a replay prints: the same project
	// run from another root is another label, and must not replay this one's. Empty for a run that is no child.
	labeled := append(slices.Clone(facts), "label="+os.Getenv(projectLabelVariable))
	keyParts, runCacheKeyPartsOfError := program.RunCacheKeyPartsOf(os.Args[1:], anchor.Resolved(), labeled...)
	if runCacheKeyPartsOfError != nil {
		return nil
	}
	key := keyParts.Key()

	prepareCacheDirectory(root)
	directory := cache_directory.Of(root)
	printPreviousNotes(directory)
	// The run before this one may still be writing the table after answering its caller, and reading
	// before its renames would miss what it recorded, or see its run beside the findings before them. So the
	// lock is held shared across every file read here. See table_lock.go.
	release, _ := holdTableReadLock(directory, tableWriterWait)
	identity := cacheTableIdentity()
	// This invocation's run first and alone: a replay needs nothing else, and the run is a sixth of the
	// table's bytes where the whole table was all of them (#45ekc65).
	//
	// A child of discovery's is named by its engine too. A Go workspace and the format project at one root are
	// given the same arguments, and they share this directory's table, so one name would hold whichever
	// recorded last and each would miss the other's run every time.
	invocationArguments := os.Args[1:]
	if engine := os.Getenv(projectEngineVariable); engine != "" {
		invocationArguments = append(slices.Clone(invocationArguments), projectEngineVariable+"="+engine)
	}
	invocation := program.CacheTableInvocation(invocationArguments)
	table, readCacheTableError := program.ReadCacheTable(
		directory,
		identity,
		program.CacheTableSections{Runs: []string{invocation}},
		anchor,
	)
	stored := table.Runs[invocation]
	// Every input's stat is noted as it is checked, so a run that misses hands them to the content pack rather
	// than statting each file again (#kdee854). See program.StatSnapshot.
	runCacheCheckStats = program.NewStatSnapshot()
	checked := stored.CheckNoting(key, runCacheCheckStats)
	if checked == nil {
		release()
		replayRunCache(stored, anchor)
	}
	var sectionsError error
	if sections.Findings || sections.Signatures || sections.Types {
		var reused *program.CacheTable
		reused, sectionsError = program.ReadCacheTable(directory, identity, sections, anchor)
		table.Findings, table.Signatures, table.Types = reused.Findings, reused.Signatures, reused.Types
	}
	release()
	for _, readError := range []error{readCacheTableError, sectionsError} {
		if errors.Is(readError, program.ErrCacheTableUnreadable) ||
			errors.Is(readError, program.ErrCacheTablePartlyKept) {
			// Said once, before the recording starts, so it reaches the terminal and never a replay. A table
			// thrown away on every run would otherwise look like a cache that is merely cold.
			fmt.Fprintf(
				accountOutput(streams.Errors),
				"note: %v; this run starts cold and writes what it drops anew\n",
				readError,
			)
			showWarning()
		}
	}

	session := &runCacheSession{
		directory:  directory,
		invocation: invocation,
		table:      table,
		key:        key,
		keyParts:   keyParts,
		missed:     runCacheMiss(stored, checked, keyParts, readCacheTableError),
		anchor:     anchor,
		recorder:   program.NewInputRecorder(),
		// See startRunCacheClock: before discovery reads anything, after the cache directory exists.
		readSince:      runCacheClock,
		clockUnstamped: runCacheClockUnstamped,
	}
	if startError := session.stdout.start(&os.Stdout, streams.Output); startError != nil {
		return nil
	}
	if startError := session.stderr.start(&os.Stderr, streams.Errors); startError != nil {
		session.stdout.stop(&os.Stdout, streams.Output)
		return nil
	}
	activeRunCache = session
	return session
}

// prepareCacheDirectory creates the project's cache directory before anything is read or recorded, and
// warns once when the project's .gitignore does not ignore it.
//
// Created first because the run cache records the project root's directory as an input, at modification
// time. Creating `.cache` moves the root's time, and doing it after the inputs were recorded would make
// the first replay of every project a miss that looks merely cold. Writes inside `.cache/cohere` move only
// that directory's time, which no tsconfig enumerates, since a wildcard never matches a dot directory.
//
// A directory this run creates ignores itself (cache_directory.Make), so the warning is said only of one that
// was there already without that file, as an older cohere made it. The warning is a warning rather than a
// refusal: the cache is still the project's to keep, and the line to add is named so it is one edit away. What
// it must never be is a silent write into tracked space.
var prepareCacheDirectoryOnce sync.Once

// startRunCacheClock starts the run cache's clock: after the cache directory exists, since creating it moves the
// project root's time, and before discovery lists the tree. The clock was taken at the session's start, in
// beginRunCache, until discovery's listings came to serve the build's enumeration (#bjv0tg4); discovery runs
// before the session, so a file created between its listing and the session's start was missing from the
// enumeration while its directory's time already counted it, and the next run replayed over it
// (TestAFileCreatedAfterDiscoveryIsNeverReplayedOver). An earlier clock costs only a run whose inputs change in
// that moment, which is then not recorded.
func startRunCacheClock(location projectLocation, locateError error) {
	eligible := locateError == nil && runCacheEligible()
	if eligible {
		prepareCacheDirectory(location.Root)
	}
	// Any run that keeps a cache may read the libraries' records, eligible for replay or not.
	if locateError == nil && !cacheOff {
		prepareNestedCacheDirectories(writeRepositoryRoot(location.ArgumentBase, location.Root))
	}
	runCacheClock = time.Now()
	if !eligible {
		return
	}
	directory := cache_directory.Of(location.Root)
	// A test instrument: a directory no stamp can be taken in, as a cache directory that can't be written is.
	// See TestARunWhoseClockIsNoStampIsNeverRecorded.
	if os.Getenv("COHERE_TEST_NO_CLOCK_STAMP") != "" {
		directory = filepath.Join(directory, "no-clock-stamp-here")
	}
	stamp, filesystemClockError := filesystemClock(directory)
	if filesystemClockError != nil {
		runCacheClockUnstamped = filesystemClockError.Error()
		return
	}
	runCacheClock = stamp
}

// clockTickLongest is how long filesystemClock waits for the filesystem's time to move: past the 4 to 10ms tick
// Linux stamps with, short of a filesystem that stamps whole seconds, whose runs then go unrecorded rather than
// wait.
const clockTickLongest = 25 * time.Millisecond

// filesystemClock is the run cache's clock as the filesystem stamps time, on a tick of its own, or an error when no
// stamp can be taken in directory. There is no fallback to the process's clock: that is the clock this replaced,
// which lags a Linux stamp, so a run whose clock is no stamp goes unrecorded instead.
//
// The inputs a recording must not have changed since are judged by their stamps, so the clock comes from the same
// source. Linux stamps a file from a coarse kernel clock that can lag the process's by a tick, so a directory a
// file was added to just after a process clock started carried a time from before it, and the run was recorded
// over the file and replayed the next time (#6cmmpm1, on Home). A change in the clock's own tick carries the
// clock's own time, so the record refuses an input stamped at the clock, not only after it (RecordRunCache).
//
// The clock is the first stamp later than one taken just before it, so everything this run wrote while preparing,
// a project's first `.cache` among it, is stamped before the clock rather than in its tick, where it would refuse
// the run's own record. On a filesystem with nanosecond stamps the second is later at once; on Linux the wait is
// at most a tick.
func filesystemClock(directory string) (time.Time, error) {
	first, filesystemStampError := filesystemStamp(directory)
	if filesystemStampError != nil {
		return time.Time{}, filesystemStampError
	}
	deadline := time.Now().Add(clockTickLongest)
	for {
		next, filesystemStampError := filesystemStamp(directory)
		if filesystemStampError != nil {
			return time.Time{}, filesystemStampError
		}
		if next.After(first) || time.Now().After(deadline) {
			return next, nil
		}
		time.Sleep(500 * time.Microsecond)
	}
}

// filesystemStamp is the time directory's filesystem gives a file created now, read from one created for the
// purpose and removed. A file of this run's own, rather than one every run shares, so another run writing at the
// same moment can't move this run's clock later than its start.
func filesystemStamp(directory string) (time.Time, error) {
	stamp, createError := os.CreateTemp(directory, ".clock-*")
	if createError != nil {
		return time.Time{}, fmt.Errorf("no file could be created in %s to read the filesystem's time from: %w",
			directory, createError)
	}
	information, statError := stamp.Stat()
	// Ignored: the stamp was read or it wasn't, and a file left behind is one empty file in the cache directory.
	_ = stamp.Close()
	// Ignored: as above, a stamp that stays is an empty file in the cache directory, which nothing reads.
	_ = os.Remove(stamp.Name())
	if statError != nil {
		return time.Time{}, fmt.Errorf(
			"the file created in %s to read the filesystem's time from could not be read: %w",
			directory,
			statError,
		)
	}
	return information.ModTime(), nil
}

func prepareCacheDirectory(root string) {
	prepareCacheDirectoryOnce.Do(func() {
		if createDirectoryError := cache_directory.Make(cache_directory.Of(root)); createDirectoryError != nil {
			fmt.Fprintf(
				accountOutput(streams.Errors),
				"note: the cache directory %s could not be created: %v\n",
				cache_directory.Of(root),
				firstLine(createDirectoryError.Error()),
			)
			showWarning()
			return
		}
		ignoreFile := filepath.Join(root, gitignore.IgnoreFileName)
		// Asked as git asks it, from the repository's root down to the project, so a repository whose root
		// `.gitignore` ignores `.cache/` silences it for every project beneath, one of discovery's children among
		// them. Rooted at the project, every such child said its cache would be tracked, which git would not do.
		repository := ignoreRulesRoot(root)
		matcher, newGitignoreError := gitignore.New(repository)
		if newGitignoreError != nil {
			return
		}
		projectFromRepository, relativePathError := filepath.Rel(repository, root)
		if relativePathError != nil {
			return
		}
		tablePath := filepath.ToSlash(filepath.Join(projectFromRepository, ".cache", "cohere", "findings.gob"))
		if ignored, _, ignoredPathError := matcher.IgnoredPath(
			tablePath,
			false,
		); ignoredPathError == nil && !ignored {
			fmt.Fprintf(
				accountOutput(streams.Errors),
				"note: %s does not ignore .cache/, so cohere's cache in %s would be tracked: add the line `.cache/` to it\n",
				ignoreFile,
				cache_directory.Of(root),
			)
			showWarning()
		}
	})
}

// ignoreRulesRoot is the repository whose ignore rules govern root: the nearest directory at or above it holding a
// `.git`, a clone's directory or a worktree's or submodule's file, and root itself when none does, as a fixture or a
// tarball is read on its own.
func ignoreRulesRoot(root string) string {
	for directory := filepath.Clean(root); ; directory = filepath.Dir(directory) {
		if format_files.HasOwnRepository(directory) {
			return directory
		}
		if filepath.Dir(directory) == directory {
			return filepath.Clean(root)
		}
	}
}

// readFormatSection is the format record's section of this root's table, nil when there is none or the
// table was discarded. The caller decides whether its key still holds.
func readFormatSection(root string) *program.FormatSection {
	if cacheOff {
		return nil
	}
	release, _ := holdTableReadLock(cache_directory.Of(root), tableWriterWait)
	// Ignored: a table missing or unreadable holds no format record, which is the nil this answers, and costs the
	// run a format of files already formatted, never a skip; beginRunCache is where a discarded table is said.
	table, _ := program.ReadCacheTable(
		cache_directory.Of(root),
		cacheTableIdentity(),
		program.CacheTableSections{Formatted: true},
		program.NewPathAnchor(root),
	)
	release()
	return table.Formatted
}

// writeFormatSection replaces the format record's file and touches no other. The record is saved where the
// format phase ends, mid-run, for any run that formats, writing or not; its own file makes that a write of
// the record alone, about a megabyte on ahra, where it used to be a read and a write of the whole table
// (#45ekc65).
func writeFormatSection(root string, section *program.FormatSection) error {
	if cacheOff {
		return nil
	}
	prepareCacheDirectory(root)
	directory := cache_directory.Of(root)
	release := holdTableLock(directory)
	defer release()
	return program.WriteCacheTable(directory, &program.CacheTable{Formatted: section}, cacheTableIdentity(),
		program.CacheTableSections{Formatted: true}, program.NewPathAnchor(root))
}

// dumpCacheTable is `--cache-dump`: what this project's table holds, read by the same build that would use
// it, so a table this binary would discard says so rather than printing as though it were in use.
func dumpCacheTable(location projectLocation) error {
	directory := cache_directory.Of(location.Root)
	release, _ := holdTableReadLock(directory, tableWriterWait)
	table, readCacheTableError := program.ReadCacheTable(
		directory,
		cacheTableIdentity(),
		program.EveryCacheTableSection,
		program.NewPathAnchor(location.Root),
	)
	release()
	if readCacheTableError != nil {
		if errors.Is(readCacheTableError, os.ErrNotExist) {
			fmt.Fprintf(streams.Output, "no cache table for %s (looked in %s)\n", location.Root, directory)
			return nil
		}
		fmt.Fprintf(streams.Output, "%v\n", readCacheTableError)
		if !errors.Is(readCacheTableError, program.ErrCacheTablePartlyKept) {
			return nil
		}
	}
	program.DumpCacheTable(streams.Output, directory, table, cacheTableIdentity())
	return nil
}

// cacheTableIdentity is the build a table must have been written by to be read. Anything about the binary
// that can change what a cached answer means is in it, and the keys inside the table cover the rest.
func cacheTableIdentity() program.CacheTableIdentity {
	provenance := release.Current()
	return program.CacheTableIdentity{
		SelfCommit:     provenance.SelfCommit,
		CompilerCommit: provenance.CompilerCommit,
		GoToolchain:    provenance.GoToolchain,
		Platform:       provenance.Platform,
	}
}

// runCacheMiss says why a run was not replayed whole, empty when it was about to be: no run on record for these
// arguments, one under another key and which part moved, or the input that changed. A table thrown away is
// said first, since then nothing else was read.
func runCacheMiss(stored *program.RunCache, checked error, parts program.RunCacheKeyParts, readError error) string {
	if checked == nil {
		return ""
	}
	discarded := ""
	if errors.Is(readError, program.ErrCacheTableUnreadable) || errors.Is(readError, program.ErrCacheTablePartlyKept) {
		discarded = firstLine(readError.Error()) + "; "
	}
	switch {
	case stored == nil:
		return discarded + "no run is on record for these arguments"
	case stored.Key != parts.Key():
		return discarded + "the run on record was taken under another key: " + parts.Changed(stored.KeyParts)
	}
	return discarded + strings.TrimPrefix(checked.Error(), program.ErrRunCacheMiss.Error()+": ")
}

// replayRunCache prints a recorded run, framed so its durations cannot be read as this run's, and
// exits with its exit code. Its paths are printed as this run spells the root (see recordedOutput).
func replayRunCache(stored *program.RunCache, anchor program.PathAnchor) {
	recorded := time.Unix(0, stored.RecordedUnixNanoseconds).Format("15:04:05")
	account := accountOutput(streams.Output)
	fmt.Fprintf(account,
		"cached: no input has changed since the run at %s, so graph, types and lint did not run; its verdict follows\n",
		recorded)
	streams.Output.Write(replayLines(spelledOutput(stored.Output, anchor), recorded))
	// The recording's stdout before its stderr, the order a replay always printed them in.
	streams.Show(streams.Output)
	streams.Errors.Write(spelledOutput(stored.Errors, anchor))
	streams.Show(streams.Errors)
	// This invocation's, never the recording's (see ignore_account.go).
	writeDiscoveredIgnores(streams.Output)
	fmt.Fprintf(account, "phases: replayed the run at %s · fix, types and lint did not run\n", recorded)
	fmt.Fprintf(
		account,
		"  this run: %s, after checking %d inputs\n",
		round(time.Since(processStart)),
		len(stored.Inputs),
	)
	fmt.Fprintf(account, "  %s\n", activeMemoryPolicy.line())

	// The footer is this replay's, rendered from the recorded run's summary: its findings and its counts,
	// with this run's time, nothing checked fresh, every file answered by the cache and nothing rewritten.
	// The recorded run's own footer is not in the recording, since it would claim that run's time.
	var summary runSummary
	if len(stored.Summary) > 0 && json.Unmarshal(spelledOutput(stored.Summary, anchor), &summary) == nil {
		summary = replayedSummary(summary, time.Since(processStart))
		// The run on record kept none, since what discovery found is this run's to say.
		summary.IgnorePatterns = discoveredIgnores
		summary.Findings += unusedIgnoreCount(discoveredIgnores)
		switch activeOutput.Mode {
		case outputVerbose:
			fmt.Fprintln(
				streams.Output,
				footer(summary, activeOutput.Style, footerOptions{Phases: activeOutput.Phases, Verbose: true}),
			)
		case outputJson:
			writeSummaryLine(streams.Output, summaryAsJson(summary))
		default:
			if !handSummaryToRoot(summary) {
				fmt.Fprintln(
					streams.Output,
					footer(summary, activeOutput.Style, footerOptions{Phases: activeOutput.Phases}),
				)
			}
		}
	}
	exitProcess(stored.ExitCode)
}

// replayedSummary is a recorded run's summary as its replay says it: the same findings and gaps, this
// replay's time, no phase run, nothing checked fresh, every file in scope answered by the cache, and
// nothing rewritten or walked. The recorded run's phases that ran are dropped, since none ran now; the
// ones that did not are kept, since a phase the recorded run never reached is a gap in the verdict this
// replay repeats.
func replayedSummary(recorded runSummary, elapsed time.Duration) runSummary {
	replayed := recorded
	replayed.Total = elapsed
	replayed.Graph, replayed.Formatting = 0, 0
	replayed.Phases = nil
	for _, record := range recorded.Phases {
		if record.Outcome == outcomeSkipped || record.Outcome == outcomeNotReached {
			replayed.Phases = append(replayed.Phases, record)
		}
	}
	// Every file in scope was answered by the cache, so the count says so: --json read 0 here, the number a run
	// that replayed nothing reads, and a reader took a whole replay for a full check (#547dhjz).
	replayed.Cache = cacheUse{Replayed: true, FilesReplayed: recorded.FilesInScope}
	// The engine's cache was not consulted by a replay, which started no engine, so what it did is the recorded run's.
	replayed.PackageCache = nil
	// Nor did a replay raise its heap: the raises were the recorded run's (#5shepvt).
	replayed.MemoryRaisedToBytes = nil
	// Cached is what the recorded run accounted for, checked or cached: a file it never examined, the cache cannot
	// answer for either, and the recorded gap still names it (#1hva1ya).
	replayed.FilesChecked, replayed.FilesCached = 0, recorded.FilesChecked+recorded.FilesCached
	replayed.Changed = nil
	replayed.Nodes = 0
	return replayed
}

// declareRunCacheInputs marks the build as having succeeded, and adds inputs the command reads itself.
func declareRunCacheInputs(files ...string) {
	if activeRunCache == nil {
		return
	}
	activeRunCache.declared = true
	activeRunCache.extraFiles = append(activeRunCache.extraFiles, files...)
}

// declareLintConfigInputs adds the lint config and every file it extends to this run's inputs, each present or
// absent by what is on disk. Zero config is the default settings file missing (#e0tybvv): declared as a file the
// run read, recording stat'ed a file that was not there and refused the record, so a zero-config project never
// replayed. Its absence is the input, since creating the file changes what runs.
func declareLintConfigInputs(path string) {
	session := activeRunCache
	if session == nil {
		return
	}
	for _, source := range configuration.SourcesOnDisk(lintConfigSources(path)) {
		if _, statError := os.Stat(source); statError == nil {
			session.extraFiles = append(session.extraFiles, source)
		} else {
			session.extraAbsent = append(session.extraAbsent, source)
		}
	}
}

// noteFormatWalkGaps says, in the run's gaps, what a format walk the run used could not decide: a repository's index
// it fell back from (#14skmqy). Every walk a run uses is declared (declareFormatWalk), and this is called beside it,
// whether or not the run is recorded.
func noteFormatWalkGaps(enumeration format_files.Enumeration) {
	if enumeration.IndexFallback != "" && activeSummary.Gaps.IndexFallback == "" {
		activeSummary.Gaps.IndexFallback = enumeration.IndexFallback
	}
}

// declareFormatWalk adds what one format walk depends on to this run's inputs, so a run that checks
// formatting can be replayed (#13a63n3).
//
// What the walk's answer is a function of: the files it found, whose bytes decide what the formatter
// reports; every directory it listed, whose modification time moves when an entry is added, removed or
// renamed, which is also what catches a new .git, settings file or leftover Prettier config inside the
// tree; and the files it read or would read, each recorded when present, since its directory already
// covers its appearing. Those are the root's .gitmodules, the .prettierignore the walk refuses, and every
// CohereSettings.json a directory resolves its options from, with the files it extends.
//
// The git ignore files the walk read or looked for (Enumeration.IgnoreFiles: each entered directory's
// .gitignore, info/exclude, and the repository's index, whose tracked paths are never ignored) are recorded
// present or absent. A content edit to a nested .gitignore moves no directory's modification time, and
// info/exclude and the index sit in .git, a directory the walk never lists. An index the walk read whole is keyed on
// what it decides for the walk rather than on its bytes (runCacheSession.trackedIgnored).
// The formatter's own identity needs no input: the run's key covers the running binary (see beginRunCache).
func declareFormatWalk(enumeration format_files.Enumeration) {
	session := activeRunCache
	if session == nil {
		return
	}
	for _, ignoreFile := range enumeration.IgnoreFiles {
		if _, statError := os.Stat(ignoreFile); statError == nil {
			session.extraFiles = append(session.extraFiles, ignoreFile)
		} else {
			session.extraAbsent = append(session.extraAbsent, ignoreFile)
		}
	}
	if enumeration.Index != "" {
		session.trackedIgnored = append(session.trackedIgnored,
			trackedIgnoredIndex{index: enumeration.Index, root: enumeration.IndexRoot, scope: enumeration.IndexScope})
	}
	read := []string{
		filepath.Join(enumeration.Root, ".gitmodules"),
		filepath.Join(enumeration.Root, ".prettierignore"),
	}
	for _, directory := range enumeration.Directories {
		read = append(read, filepath.Join(directory, format_options.SettingsFileName))
	}
	// The settings the root itself resolves to can sit above it, in a directory the walk never listed, and so
	// can the directories between: a settings file created in one of those would take over the options.
	if resolution, resolveError := format_options.Resolve(
		enumeration.Root,
	); resolveError == nil && resolution.Source != "" {
		read = append(read, resolution.Source)
		for directory := filepath.Clean(
			enumeration.Root,
		); directory != filepath.Dir(resolution.Source); directory = filepath.Dir(directory) {
			session.extraDirectories = append(session.extraDirectories, directory)
			if parent := filepath.Dir(directory); parent == directory {
				break
			}
		}
	}
	for _, path := range read {
		if _, statError := os.Stat(path); statError != nil {
			continue
		}
		if filepath.Base(path) == format_options.SettingsFileName {
			session.extraFiles = append(session.extraFiles, configuration.SourcesOnDisk(lintConfigSources(path))...)
			continue
		}
		session.extraFiles = append(session.extraFiles, path)
	}
	session.extraFiles = append(session.extraFiles, enumeration.Files...)
	session.extraDirectories = append(session.extraDirectories, enumeration.Directories...)
}

// declineRunCache records why this run must not be replayed. The first reason stands.
func declineRunCache(reason string) {
	if activeRunCache != nil && activeRunCache.declined == "" {
		activeRunCache.declined = reason
	}
}

// abandonRunCache stops recording without writing anything, for a run that failed as a tool. It
// restores the real streams first, so the error about to be printed reaches the terminal.
func abandonRunCache() {
	session := activeRunCache
	if session == nil {
		return
	}
	activeRunCache = nil
	session.stdout.stop(&os.Stdout, streams.Output)
	session.stderr.stop(&os.Stderr, streams.Errors)
}

// finishRunCache ends a run whose exit is a verdict about the tree: it records the run if it may, then
// exits with the code. Every verdict exit goes through here, or the cache would record a run with the
// wrong exit code, and a failing tree replayed as exit 0 passes CI while printing a failure. The record keeps
// the tree's own code and the caller is answered with ignoreVerdict's, since discovery says its dead ignores
// again on every run, a replayed one included.
func finishRunCache(exitCode int) {
	finishOpen()
	session := activeRunCache
	if session != nil {
		activeRunCache = nil
		session.stdout.stop(&os.Stdout, streams.Output)
		session.stderr.stop(&os.Stderr, streams.Errors)
		// The report is out, so the caller can have its answer while the run is recorded. See sendVerdict.
		// The answer is the code the process will exit with, which is 1 when the report never reached the
		// terminal, while the record keeps the tree's own code: a replay that prints it whole exits as the tree
		// does. The table's lock is taken first and held until the table is in place, so a run the caller
		// starts next waits for this write rather than reading around it. See table_lock.go.
		release := holdTableLock(session.directory)
		answered := sendVerdict(streams.Finish(ignoreVerdict(exitCode)))
		var recorded *program.RunCache
		if session.declared && session.declined == "" {
			recorded = session.record(exitCode)
		}
		if answered {
			graduateSignatures(session.directory, session.shapesGraph, session.shapes)
		}
		session.write(recorded)
		release()
	} else if record := pendingTypes; record != nil {
		// A run nothing records still leaves the types section for the next, after its caller has the answer.
		pendingTypes = nil
		release := holdTableLock(record.directory)
		if sendVerdict(streams.Finish(ignoreVerdict(exitCode))) {
			record.computedShapes += graduateSignatures(record.directory, record.shapesGraph, record.shapes)
		}
		record.write()
		release()
	}
	exitProcess(exitCode)
}

// signatureGraduationBudget is how long a run spends giving content-keyed shapes real signatures after its
// verdict is out. The table's lock is held throughout, so a run started at once waits this long at most.
var signatureGraduationBudget = time.Second

// graduateSignatures gives the shapes still keyed on content real declaration signatures, for at most
// signatureGraduationBudget, and returns how many entries changed, the abandoned one included (#9knyr86).
//
// Only after the verdict reached the dispatcher, which is what lets the emit's cost go unseen: a run nobody
// answered early is still a caller's wait. A project with no build info used to keep every shape keyed on
// content, so a comment in one file re-ran its importers' shape-keyed rules and re-checked their types; on ahra,
// 3,974 of 3,976 files, and an edit run's types phase of about 250ms against about 50ms once they graduate.
func graduateSignatures(directory string, graph *program.Graph, shapes map[string]program.SignatureEntry) int {
	if graph == nil || shapes == nil {
		return 0
	}
	graduated, abandoned := graph.GraduateSignatures(
		shapes,
		time.Now().Add(signatureGraduationBudget),
		signatureGraduationBudget/2,
	)
	if abandoned != "" {
		cacheNote(
			directory,
			fmt.Sprintf("%s's declaration emit ran past %s, so its shape stays keyed on content until its bytes change",
				abandoned, signatureGraduationBudget/2),
		)
		return graduated + 1
	}
	return graduated
}

// cacheNote says something about the cache in directory: on stderr while the caller is still waiting, and to
// the next run once it has its verdict.
func cacheNote(directory string, text string) {
	if !verdictSent {
		fmt.Fprintf(accountOutput(streams.Errors), "note: %s\n", text)
		showWarning()
		return
	}
	file, openError := os.OpenFile(previousNotesPath(directory), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if openError != nil {
		return
	}
	// Ignored: the caller has its verdict and its terminal may hold someone else's prompt, so a note that cannot be
	// kept for the next run has nowhere left to be said, as one whose file cannot be opened above.
	_, _ = fmt.Fprintln(file, text)
	_ = file.Close() // Ignored: as the write's, above.
}

// previousNotesPath is where a run that answered its caller early keeps what it had to say afterward.
func previousNotesPath(directory string) string {
	return filepath.Join(directory, "notes-after-return.txt")
}

// printPreviousNotes says, once, what the last run recorded after it had already returned, and clears it.
// A cache write that failed in the background otherwise fails silently forever, the saving quietly gone.
func printPreviousNotes(directory string) {
	path := previousNotesPath(directory)
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return
	}
	// Ignored: notes that cannot be cleared are said again by the next run, which repeats what is true rather
	// than losing it.
	_ = os.Remove(path)
	for line := range strings.SplitSeq(strings.TrimSpace(string(contents)), "\n") {
		if line != "" {
			fmt.Fprintf(accountOutput(streams.Errors), "note: after the last run returned, %s\n", line)
		}
	}
	showWarning()
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

// printInvocation prints a line that describes this invocation rather than the tree. While a recording is in
// progress each of its lines is tagged (see invocationTag); without one it prints exactly what it always did.
func printInvocation(out *bufio.Writer, format string, arguments ...any) {
	printTagged(out, invocationTag, fmt.Sprintf(format, arguments...))
}

// printProvenance prints a line that is true of the tree and was produced by a phase, tagged while a recording
// is in progress (see provenanceTag).
func printProvenance(out *bufio.Writer, format string, arguments ...any) {
	printTagged(out, provenanceTag, fmt.Sprintf(format, arguments...))
}

// printTagged prints text, tagged when a recording is in progress. The text is whole before it is tagged, so every
// line of it starts with the tag however the stream later splits its writes.
func printTagged(out *bufio.Writer, tag []byte, text string) {
	printed := text
	if activeRunCache != nil {
		printed = tagLines(text, tag)
	}
	fmt.Fprint(out, printed)
}

// tagLines is text with tag at the start of every line. A text that ends without a newline ends on a tagged
// line, and an empty one stays empty.
func tagLines(text string, tag []byte) string {
	var tagged strings.Builder
	for line := range strings.SplitAfterSeq(text, "\n") {
		if line != "" {
			tagged.Write(tag)
			tagged.WriteString(line)
		}
	}
	return tagged.String()
}

// replayLines is a recording made printable: each provenance-tagged line "label: rest" becomes
// "label (from the cached run at recorded): rest", and no tag survives.
func replayLines(output []byte, recorded string) []byte {
	var replayed bytes.Buffer
	for _, line := range bytes.SplitAfter(output, []byte("\n")) {
		rest, tagged := bytes.CutPrefix(line, provenanceTag)
		if !tagged {
			replayed.Write(line)
			continue
		}
		label, detail, found := bytes.Cut(rest, []byte(": "))
		if !found {
			// A line with no label still must not claim to be current, so the source goes in front.
			fmt.Fprintf(&replayed, "(from the cached run at %s) %s", recorded, rest)
			continue
		}
		fmt.Fprintf(&replayed, "%s (from the cached run at %s): %s", label, recorded, detail)
	}
	return replayed.Bytes()
}

// rootTag stands for the project root in a recorded run's output, so a replay prints its paths as the replaying
// run spells the root rather than as the recording run did (#547dhjz).
var rootTag = []byte("\x1ecohere-root\x1e")

// recordedOutput is output with the project root, by either the spelling this run was given or its resolved
// target, replaced by rootTag. Only a whole path component is replaced: a root of /work/app leaves
// /work/application alone.
func recordedOutput(output []byte, anchor program.PathAnchor) []byte {
	recorded := output
	for _, root := range []string{anchor.Root(), anchor.Resolved()} {
		if root == "" {
			continue
		}
		recorded = replaceRoot(recorded, []byte(root), rootTag)
	}
	return recorded
}

// spelledOutput is a recording's output with rootTag replaced by the root as this run spells it.
func spelledOutput(output []byte, anchor program.PathAnchor) []byte {
	return bytes.ReplaceAll(output, rootTag, []byte(anchor.Root()))
}

// replaceRoot replaces each occurrence of root that is a whole path: one that starts where a path does, not
// after a separator or inside a name (/tmp/x inside /private/tmp/x is not a root), and ends where a path
// component does, at a separator, at the end, or at a byte no name would continue with, such as a space, a
// colon or a comma.
func replaceRoot(output []byte, root []byte, replacement []byte) []byte {
	var replaced bytes.Buffer
	remaining := output
	for {
		position := bytes.Index(remaining, root)
		if position < 0 {
			replaced.Write(remaining)
			return replaced.Bytes()
		}
		end := position + len(root)
		if position > 0 && !endsEscape(remaining[:position]) &&
			(remaining[position-1] == '/' || remaining[position-1] == '\\' || continuesName(remaining[position-1])) ||
			end < len(remaining) && continuesName(remaining[end]) {
			replaced.Write(remaining[:end])
			remaining = remaining[end:]
			continue
		}
		replaced.Write(remaining[:position])
		replaced.Write(replacement)
		remaining = remaining[end:]
	}
}

// endsEscape reports whether text ends with a terminal's color sequence, ESC [ digits and semicolons, m, which a
// colored report prints right before a path.
func endsEscape(text []byte) bool {
	// A color sequence is a few bytes, so only the tail is searched: a report with thousands of paths and no
	// color would otherwise be scanned back to its start for each one.
	tail := text[max(0, len(text)-16):]
	start := bytes.LastIndexByte(tail, 0x1b)
	if start < 0 || len(tail)-start < 3 || tail[start+1] != '[' || tail[len(tail)-1] != 'm' {
		return false
	}
	for _, character := range tail[start+2 : len(tail)-1] {
		if character != ';' && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// continuesName reports whether a byte after a root would make it part of a longer name.
func continuesName(next byte) bool {
	return next == '-' || next == '_' || next == '.' || next >= '0' && next <= '9' || next >= 'A' && next <= 'Z' ||
		next >= 'a' && next <= 'z' || next >= 0x80
}

// attachFindingsCache gives this run's graph the findings cache, the run cache's second layer: when
// the run as a whole cannot be replayed, files whose bytes are unchanged replay their cacheable rules'
// findings and coverage, and only their uncacheable rules are walked. See program.FindingsReuse.
//
// Only a run being recorded gets one, so it is exactly as narrow as the run cache.
//
// Mutates: graph the caller's graph is what lints and checks, so the caches it reads are attached to it here
func attachFindingsCache(graph *program.Graph, location projectLocation) {
	session := activeRunCache
	if session == nil || graph == nil {
		return
	}
	parts, findingsCacheKeyError := findingsCacheKey(graph, location)
	if findingsCacheKeyError != nil {
		// Without a key nothing can be proven unchanged, so nothing is replayed or recorded.
		return
	}
	key := sha256.Sum256([]byte(parts.Key()))
	session.findingsKeyParts = parts
	switch previous := session.table.Findings; {
	case previous == nil:
		session.findingsMissed = "no findings are on record"
	case previous.Key != key:
		session.findingsMissed = "the findings on record were taken under another key: " +
			parts.Changed(previous.KeyParts)
	}
	session.findings = program.NewFindingsReuse(key, session.table.Findings, session.anchor)

	// Shapes cost a declaration emit for each file whose bytes changed, so they are computed only when some
	// rule is keyed on them. They start from the table's, then from the compiler's own buildinfo, so only
	// what changed since either is computed.
	if anyRuleKeyedOnShapes(registry.All()) {
		shapes, _ := graph.Signatures(context.Background(), graph.SeedSignatures(session.table.Signatures))
		graph.Shapes = shapes
		session.shapes, session.shapesGraph = shapes, graph
	}
	graph.FindingsReuse = session.findings
	// An entry recorded by a run that did not measure readiness was never measured, so a run that does must
	// miss on it rather than replay it as clean (#drbrp8c).
	if graph.Readiness != nil {
		session.findings.MeasureReadiness()
	}
	if typesKey, typesCacheKeyError := typesCacheKey(graph, location); typesCacheKeyError == nil {
		session.types = program.NewTypeDiagnosticsReuse(session.table.Types, typesKey)
	}
}

// carryCaches gives the graph rebuilt after a fix rewrite what the graph it replaces was given: the findings
// cache, and the shapes, recomputed only for the files whose bytes the fixer changed (#891h54d).
//
// The rebuild used to leave both off. With no shapes the types section cannot replay, so a run whose fixer
// rewrote one file full-checked the whole program, 380 to 1,270ms on ahra against 42 to 72ms replayed; with
// no findings cache, lint walked every rule over every file. Shapes are keyed per file on its bytes
// (Graph.Signatures), so seeding from the replaced graph's recomputes exactly the rewritten files, and the
// findings cache is per file on bytes and fingerprints, so the one cache serves both graphs.
//
// Mutates: rebuilt the graph the caller lints after the fix is handed the caches the replaced graph had
func carryCaches(replaced *program.Graph, rebuilt *program.Graph) {
	rebuilt.FindingsReuse = replaced.FindingsReuse
	if replaced.Shapes == nil {
		return
	}
	shapes, computed := rebuilt.Signatures(context.Background(), replaced.Shapes)
	rebuilt.Shapes = shapes
	if session := activeRunCache; session != nil && session.shapes != nil {
		session.shapes, session.shapesGraph = shapes, rebuilt
	}
	if record := pendingTypes; record != nil {
		record.shapes, record.shapesGraph = shapes, rebuilt
		record.computedShapes += computed
	}
}

// activeTypesReuse is the types section a check in this run reads and records, nil when the cache is off
// or the run never reached a check.
func activeTypesReuse() *program.TypeDiagnosticsReuse {
	if activeRunCache != nil {
		return activeRunCache.types
	}
	if pendingTypes != nil {
		return pendingTypes.reuse
	}
	return nil
}

// attachTypesCache gives a run the run cache does not record the types section, and the shapes it is keyed
// on. A recorded run already has both from attachFindingsCache.
//
// Mutates: graph the caller's graph is what checks, so the shapes the types section is keyed on are set on it
func attachTypesCache(graph *program.Graph, location projectLocation) {
	if cacheOff || activeRunCache != nil || graph == nil {
		return
	}
	typesKey, typesCacheKeyError := typesCacheKey(graph, location)
	if typesCacheKeyError != nil {
		return
	}
	prepareCacheDirectory(location.Root)
	directory := cache_directory.Of(location.Root)
	release, _ := holdTableReadLock(directory, tableWriterWait)
	anchor := program.NewPathAnchor(location.Root)
	// Ignored: a table missing or unreadable reads as empty, so the types section and the shapes start cold and are
	// written whole; beginRunCache is where a discarded table is said.
	table, _ := program.ReadCacheTable(
		directory,
		cacheTableIdentity(),
		program.CacheTableSections{Signatures: true, Types: true},
		anchor,
	)
	release()
	record := &typesRecord{
		directory: directory,
		anchor:    anchor,
		reuse:     program.NewTypeDiagnosticsReuse(table.Types, typesKey),
	}
	record.shapes, record.computedShapes = graph.Signatures(
		context.Background(),
		graph.SeedSignatures(table.Signatures),
	)
	record.shapesGraph = graph
	graph.Shapes = record.shapes
	pendingTypes = record
}

// anyRuleKeyedOnShapes reports whether any rule declares rule.TypeReachShapes.
func anyRuleKeyedOnShapes(rules []rule.Entry) bool {
	for _, subject := range rules {
		if subject.TypeReach == rule.TypeReachShapes {
			return true
		}
	}
	return false
}

// findingsCacheKey covers everything a cacheable rule's answer depends on beyond a file's own bytes:
// the binary, the project root, the bytes of every file in the lint config's and the tsconfig's extends chains,
// and the rule set in order. Any change to one makes every entry a miss at once.
//
// The tsconfig chain is in it because it decides how a file parses, and a cacheable rule walks the
// parse. Rule options come from the lint config and the root, both already here.
func findingsCacheKey(graph *program.Graph, location projectLocation) (program.RunCacheKeyParts, error) {
	facts := []string{}
	// Every file in the lint config's extends chain, as the tsconfig's chain is below: a base edited
	// alone changes which rules run and with what options.
	// Under zero config they are the house sets and the project's own file when it has one.
	lintConfigFiles := houseSources(location)
	anchor := program.NewPathAnchor(location.Root)
	// An embedded set is hashed by its text, read the way the loader reads it, as a file is.
	for _, lintConfigFile := range lintConfigFiles {
		contents, sourceContentsError := configuration.SourceContents(lintConfigFile)
		if sourceContentsError != nil {
			return program.RunCacheKeyParts{}, sourceContentsError
		}
		facts = append(facts, fmt.Sprintf("lint-config %s=%x", anchor.Stable(lintConfigFile), sha256.Sum256(contents)))
	}
	// Under zero config which sets a file gets is decided by the whole program, so a file can change sets
	// without its own bytes changing, when another file starts importing next.
	if fingerprint := graph.LintConfiguration.DetectionFingerprint(); fingerprint != "" {
		facts = append(facts, "house-sets="+fingerprint)
	}
	compilerFacts, compilerOptionsFactsError := compilerOptionsFacts(graph, location)
	if compilerOptionsFactsError != nil {
		return program.RunCacheKeyParts{}, compilerOptionsFactsError
	}
	facts = append(facts, compilerFacts...)
	names := make([]string, 0, len(registry.All()))
	for _, registered := range registry.All() {
		names = append(names, registered.Name)
	}
	facts = append(facts, "rules="+strings.Join(names, "\x00"))
	return program.RunCacheKeyPartsOf(nil, anchor.Resolved(), facts...)
}

// typesCacheKey covers what a file's semantic diagnostics depend on that no shape fingerprint sees: the
// binary, the project root, and the compiler options, as the bytes of the tsconfig and every file it extends.
// The lint config and the rules are left out, since the compiler reads neither.
func typesCacheKey(graph *program.Graph, location projectLocation) ([sha256.Size]byte, error) {
	facts, compilerOptionsFactsError := compilerOptionsFacts(graph, location)
	if compilerOptionsFactsError != nil {
		return [sha256.Size]byte{}, compilerOptionsFactsError
	}
	key, runCacheKeyError := program.RunCacheKey(nil, program.NewPathAnchor(location.Root).Resolved(), facts...)
	if runCacheKeyError != nil {
		return [sha256.Size]byte{}, runCacheKeyError
	}
	return sha256.Sum256([]byte(key)), nil
}

// compilerOptionsFacts is the tsconfig and every file in its extends chain, by path and bytes. It decides how
// each file parses, which a cacheable rule walks, and how each checks, which the types section replays.
func compilerOptionsFacts(graph *program.Graph, location projectLocation) ([]string, error) {
	facts := []string{}
	anchor := program.NewPathAnchor(location.Root)
	fileFact := func(label string, path string) error {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		facts = append(facts, fmt.Sprintf("%s %s=%x", label, anchor.Stable(path), sha256.Sum256(contents)))
		return nil
	}
	if fileFactError := fileFact("tsconfig", location.ConfigFileName); fileFactError != nil {
		return nil, fileFactError
	}
	if graph.Configuration != nil {
		for _, extended := range graph.Configuration.ExtendedSourceFiles() {
			if fileFactError := fileFact("extends", extended.AsString()); fileFactError != nil {
				return nil, fileFactError
			}
		}
	}
	return facts, nil
}
