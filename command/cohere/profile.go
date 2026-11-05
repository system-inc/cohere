package main

import (
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/system-inc/cohere/internal/sentence"
)

// profileFile is the open `--profile` file while the CPU profile is running, nil otherwise.
var profileFile *os.File

// startProfile begins writing a Go CPU profile of the rest of the run to path, for `go tool pprof`.
func startProfile(path string) error {
	file, createError := os.Create(path)
	if createError != nil {
		return fmt.Errorf("creating the profile: %w", createError)
	}
	if startCPUProfileError := pprof.StartCPUProfile(file); startCPUProfileError != nil {
		file.Close()
		return fmt.Errorf("starting the profile: %w", startCPUProfileError)
	}
	profileFile = file
	// A profiled run never returns early: the profile is something the caller reads, and it is complete
	// only once this process stops and closes it, after the verdict would have gone. Closing the
	// descriptor unanswered tells the dispatcher to wait for this process instead. See sendVerdict.
	dropVerdict()
	return nil
}

// exitProcess ends the process with code, finishing the profile first. os.Exit runs no deferred call, so
// every exit goes through here, or a profiled run would leave a truncated profile behind.
func exitProcess(code int) {
	finishOpen()
	if profileFile != nil {
		pprof.StopCPUProfile()
		if closeError := profileFile.Close(); closeError != nil {
			fmt.Fprintf(os.Stderr, "cohere: %s\n", sentence.FromError(fmt.Errorf("closing the profile: %w", closeError)))
		}
		profileFile = nil
	}
	// Every exit answers the caller, if a run cache has not already: what follows, keeping what the run read
	// in the content pack and the heap's teardown, is nothing the caller waits for. See sendVerdict.
	sendVerdict(code)
	saveContentPack()
	os.Exit(code)
}
