package main

import (
	"os"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

// packageState is what one run of cohere writes to this package's variables, everything a test that drives a run
// in-process leaves behind it. The seams a test swaps on purpose (newIgnoreScope, parallelFormatWorkers,
// swiftBinaryPlatform and the like) are not here: each test that swaps one restores it beside the swap, and
// resetProgramBuilds keeps the record of builds.
type packageState struct {
	summary                   runSummary
	output                    outputSettings
	runCache                  *runCacheSession
	runCacheClock             time.Time
	runCacheClockUnstamped    string
	runCacheCheckStats        *program.StatSnapshot
	pendingTypes              *typesRecord
	cacheOff                  bool
	open                      *openReport
	contentPack               *program.ContentPack
	contentPackRoot           string
	aheadFormatWalk           *formatWalkAhead
	discoveredIgnores         []ignoreAccount
	discoveredIgnoresWritten  bool
	discoveredListings        *program.DirectoryListings
	namedRunLeftOutExtensions []string
	folderCheckSkipped        string
	adamicReadinessRequested  bool
	summaryWritten            bool
	verdictSent               bool
	verdictFile               *os.File
}

// restorePackageStateAfter snapshots every package-level variable a run writes and puts each back when the test
// ends. A serial test that drives a run, or a piece of one, calls it first (#9b9d4x3). Go runs every serial
// top-level test before it releases the parallel ones, so whatever the last serial writer leaves is what every
// parallel reader sees, and once the tests are dealt round-robin across shards, which serial test runs last
// changes with the dealing: a test that restored nothing turned six parallel phase tests red under one shuffle
// seed, each reading a pattern timeout another test's lint report had left in activeSummary.
func restorePackageStateAfter(test *testing.T) {
	test.Helper()
	kept := capturePackageState()
	test.Cleanup(kept.restore)
}

// capturePackageState reads each variable packageState names, as it stands now.
func capturePackageState() packageState {
	return packageState{
		summary:                   activeSummary,
		output:                    activeOutput,
		runCache:                  activeRunCache,
		runCacheClock:             runCacheClock,
		runCacheClockUnstamped:    runCacheClockUnstamped,
		runCacheCheckStats:        runCacheCheckStats,
		pendingTypes:              pendingTypes,
		cacheOff:                  cacheOff,
		open:                      activeOpen,
		contentPack:               activeContentPack,
		contentPackRoot:           activeContentPackRoot,
		aheadFormatWalk:           aheadFormatWalk,
		discoveredIgnores:         discoveredIgnores,
		discoveredIgnoresWritten:  discoveredIgnoresWritten,
		discoveredListings:        discoveredListings,
		namedRunLeftOutExtensions: namedRunLeftOutExtensions,
		folderCheckSkipped:        folderCheckSkipped,
		adamicReadinessRequested:  adamicReadinessRequested,
		summaryWritten:            summaryWritten,
		verdictSent:               verdictSent,
		verdictFile:               verdictFile,
	}
}

// restore puts each variable back as the state read it.
func (state packageState) restore() {
	activeSummary, activeOutput = state.summary, state.output
	activeRunCache = state.runCache
	runCacheClock, runCacheClockUnstamped = state.runCacheClock, state.runCacheClockUnstamped
	runCacheCheckStats, pendingTypes, cacheOff = state.runCacheCheckStats, state.pendingTypes, state.cacheOff
	activeOpen, activeContentPack, activeContentPackRoot = state.open, state.contentPack, state.contentPackRoot
	aheadFormatWalk = state.aheadFormatWalk
	discoveredIgnores, discoveredIgnoresWritten = state.discoveredIgnores, state.discoveredIgnoresWritten
	discoveredListings, namedRunLeftOutExtensions = state.discoveredListings, state.namedRunLeftOutExtensions
	folderCheckSkipped, adamicReadinessRequested = state.folderCheckSkipped, state.adamicReadinessRequested
	summaryWritten, verdictSent, verdictFile = state.summaryWritten, state.verdictSent, state.verdictFile
}
