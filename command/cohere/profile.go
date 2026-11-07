package main

import (
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/system-inc/cohere/internal/sentence"
	"github.com/system-inc/cohere/internal/streams"
)

// profileFile is the open `--profile` file while the CPU profile is running, nil otherwise.
var profileFile *os.File

// startProfile begins writing a Go CPU profile of the rest of the run to path, for `go tool pprof`.
func startProfile(path string) error {
	file, createError := os.Create(path)
	if createError != nil {
		return fmt.Errorf("creating the profile: %w", createError)
	}
	if startCpuProfileError := pprof.StartCPUProfile(file); startCpuProfileError != nil {
		_ = file.Close() // Ignored: the profile never started, so nothing was written, and its start error is returned.
		return fmt.Errorf("starting the profile: %w", startCpuProfileError)
	}
	profileFile = file
	// A profiled run never returns early: the profile is something the caller reads, and it is complete
	// only once this process stops and closes it, after the verdict would have gone. Closing the
	// descriptor unanswered tells the dispatcher to wait for this process instead. See sendVerdict.
	dropVerdict()
	return nil
}

// exitProcess ends the process with code, finishing the profile first. os.Exit runs no deferred call, so
// every exit goes through here, or a profiled run would leave a truncated profile behind, and the streams a
// person reads would never be flushed.
func exitProcess(code int) {
	// A run whose path wrote no run end still says a dead ignore it fails on. See writeDiscoveredIgnores.
	if unusedIgnoreCount(discoveredIgnores) > 0 {
		writeDiscoveredIgnores(streams.Output)
	}
	finishOpen()
	if profileFile != nil {
		pprof.StopCPUProfile()
		if closeError := profileFile.Close(); closeError != nil {
			fmt.Fprintf(streams.Errors, "cohere: %s\n",
				sentence.FromError(fmt.Errorf("closing the profile: %w", closeError)))
		}
		profileFile = nil
	}
	// The streams are flushed before the caller is answered, and the answer is 1 in place of a 0 when they
	// could not be, since a report that never arrived is not a clean run (see package streams). A run cache
	// that finished first has flushed them already, and then this changes nothing. So is it 1 for a clean run
	// when discovery found an ignore that matched nothing (see ignoreVerdict).
	exitCode := streams.Finish(ignoreVerdict(code))
	// Every exit answers the caller, if a run cache has not already: what follows, keeping what the run read
	// in the content pack and the heap's teardown, is nothing the caller waits for. See sendVerdict.
	sendVerdict(exitCode)
	saveContentPack()
	os.Exit(exitCode)
}
