package main

import (
	"fmt"
	"os"

	"github.com/system-inc/cohere/internal/types/program"
)

// The content pack's command half: open the project's pack before a build reads anything, and keep what
// the run read from disk on the way out, after the caller has its answer. See program.ContentPack.

// activeContentPack is the pack this run's builds read through, and activeContentPackRoot the project it
// belongs to. Nil when the cache is off or nothing was built.
var (
	activeContentPack     *program.ContentPack
	activeContentPackRoot string
)

// openContentPack returns the project's content pack for a build, opening it once per run, or nil with
// the cache off. A pack it cannot trust is said once and read around: every file then comes from disk.
func openContentPack(root string) *program.ContentPack {
	if cacheOff {
		return nil
	}
	if activeContentPack != nil && activeContentPackRoot == root {
		return activeContentPack
	}
	// Created before anything is read, for the reason prepareCacheDirectory gives.
	prepareCacheDirectory(root)
	pack, err := program.OpenContentPack(cacheDirectory(root))
	if err != nil {
		// A fact about this invocation, never about the tree, so a replay never says it again.
		fmt.Fprintf(invocationOutput(os.Stderr), "note: %v\n", err)
	}
	activeContentPack, activeContentPackRoot = pack, root
	return pack
}

// saveContentPack keeps what this run read from disk, under the cache directory's lock so it never
// interleaves with another run's write. It runs after the verdict, so a run that answered early pays
// nothing for it, and what goes wrong is said to the next run.
func saveContentPack() {
	pack := activeContentPack
	if pack == nil {
		return
	}
	activeContentPack = nil
	tablePath := cacheTablePath(activeContentPackRoot)
	if _, _, damaged := pack.Counts(); damaged > 0 {
		cacheNote(tablePath, fmt.Sprintf("%d files in the content pack failed their checksum or bounds and were read from disk; the pack is rewritten without them", damaged))
	}
	release := holdTableLock(tablePath)
	err := pack.Save()
	release()
	if err != nil {
		cacheNote(tablePath, fmt.Sprintf("the content pack could not be written: %v", firstLine(err.Error())))
	}
}
