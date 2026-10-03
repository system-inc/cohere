package main

import (
	"fmt"
	"os"
	"runtime/pprof"
)

// profileFile is the open `--profile` file while the CPU profile is running, nil otherwise.
var profileFile *os.File

// startProfile begins writing a Go CPU profile of the rest of the run to path, for `go tool pprof`.
func startProfile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating the profile: %w", err)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		file.Close()
		return fmt.Errorf("starting the profile: %w", err)
	}
	profileFile = file
	return nil
}

// exitProcess ends the process with code, finishing the profile first. os.Exit runs no deferred call, so
// every exit goes through here, or a profiled run would leave a truncated profile behind.
func exitProcess(code int) {
	// Every exit answers the caller first, if a run cache has not already: what follows, the profile and
	// the heap's teardown, is nothing the caller waits for. See sendVerdict.
	sendVerdict(code)
	if profileFile != nil {
		pprof.StopCPUProfile()
		if err := profileFile.Close(); err != nil && !verdictSent {
			fmt.Fprintf(os.Stderr, "cohere: closing the profile: %v\n", err)
		}
		profileFile = nil
	}
	os.Exit(code)
}
