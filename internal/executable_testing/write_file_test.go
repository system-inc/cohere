package executable_testing

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// The race is between the writers' opens and anyone's forks, so the test is many of both at once: each writer forks
// too, to run what it wrote, and the forkers fork without pause. Enough of each to meet the race on Linux nearly every
// run with plain os.WriteFile, few enough that the test takes a few seconds.
const (
	writerCount   = 8
	runsPerWriter = 32
	forkerCount   = 4
)

// script is what every writer writes and runs: a script that only succeeds, so any failure is the run's own.
const script = "#!/bin/sh\nexit 0\n"

// TestAScriptWrittenWhileOthersForkRunsAtOnce: scripts written with WriteFile, each run as soon as it is written,
// all run while other goroutines start processes without pause. With os.WriteFile in WriteFile's place, a run on
// Linux fails with `text file busy`, as a test of command/cohere's once did on Home (#g93ts58): a fork between a
// write's open and its close hands the child the descriptor, and Linux refuses to exec a file open for writing.
// macOS refuses nothing, so there this passes either way.
func TestAScriptWrittenWhileOthersForkRunsAtOnce(test *testing.T) {
	test.Parallel()
	if _, statError := os.Stat("/bin/sh"); statError != nil {
		test.Skipf("the scripts this writes and runs are /bin/sh scripts, and there is no /bin/sh here: %v", statError)
	}
	truePath, lookPathError := exec.LookPath("true")
	if lookPathError != nil {
		test.Skipf("the forks beside the writes run true, and there is none on PATH: %v", lookPathError)
	}
	directory := test.TempDir()

	stop := make(chan struct{})
	var forkers sync.WaitGroup
	for range forkerCount {
		forkers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Ignored: the fork is what this goroutine is for, and whether true ran says nothing of the writes.
				_ = exec.Command(truePath).Run()
			}
		})
	}

	failures := make(chan error, writerCount*runsPerWriter)
	var writers sync.WaitGroup
	for writer := range writerCount {
		writers.Go(func() {
			for run := range runsPerWriter {
				path := filepath.Join(directory, fmt.Sprintf("script-%d-%d", writer, run))
				if writeError := WriteFile(path, []byte(script), 0o755); writeError != nil {
					failures <- fmt.Errorf("writing %s: %w", path, writeError)
					continue
				}
				if runError := exec.Command(path).Run(); runError != nil {
					failures <- fmt.Errorf("running %s as soon as it was written: %w", path, runError)
				}
			}
		})
	}
	writers.Wait()
	close(stop)
	forkers.Wait()
	close(failures)
	for failure := range failures {
		test.Error(failure)
	}
}
