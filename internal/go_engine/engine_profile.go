package go_engine

import (
	"fmt"
	"io"
	"os"
	"runtime/pprof"
)

// startProfile begins writing this process's CPU profile to path, for `go tool pprof`, and returns what ends it
// (#r33n96f). The file is created before the run reads anything, so a path that can't be written is refused with exit
// 2 before the first record, never found missing after a run that read as profiled.
//
// The stop writes the profile out and closes the file. A file that can't be closed is said on standardError, since the
// profile in it is incomplete, and the run's exit is left as the run decided: the verdict is true of the tree either
// way, as the front door's own profile leaves it (exitProcess).
func startProfile(path string) (stop func(standardError io.Writer), startError error) {
	file, createError := os.Create(path)
	if createError != nil {
		return nil, fmt.Errorf("creating the profile: %w", createError)
	}
	if startCpuProfileError := pprof.StartCPUProfile(file); startCpuProfileError != nil {
		_ = file.Close() // Ignored: the profile never started, so nothing was written, and its start error is returned.
		return nil, fmt.Errorf("starting the profile: %w", startCpuProfileError)
	}
	return func(standardError io.Writer) {
		pprof.StopCPUProfile()
		if closeError := file.Close(); closeError != nil {
			// Ignored: stderr is the only place this can be said.
			_, _ = fmt.Fprintln(standardError, refusalSentence(fmt.Errorf(
				"closing the profile %s, which is incomplete: %w", path, closeError)))
		}
	}, nil
}
