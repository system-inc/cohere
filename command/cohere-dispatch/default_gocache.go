package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/system-inc/cohere/internal/release/dispatch"
)

// boundDefaultGoCacheFlag runs the launcher as the background trim of the default Go build cache. It is
// what startBoundingDefaultGoCache starts, and only ever the sole argument, so no flag meant for cohere
// can be read as it.
const boundDefaultGoCacheFlag = "--dispatch-bound-default-go-cache"

// startBoundingDefaultGoCache starts, at most once per interval, a copy of this launcher that trims the
// default Go build cache, and does not wait for it (#3sgjy0h).
//
// The default cache is the one every member's plain `go build`, `go test` and `go vet` fills, and Go
// bounds it by age alone. The launcher runs constantly while anyone develops cohere, so it is where the
// trim lives. A run pays one stat for the check; the walk and the removals happen in the copy, so no run
// waits on them. The copy holds none of the caller's descriptors, its output going to /dev/null, so a
// caller reading this run's output to the end is never kept waiting by it either. A copy that fails says
// so in the prune log.
func startBoundingDefaultGoCache(paths dispatch.Paths) {
	due, err := dispatch.ClaimDefaultGoCacheCheck(paths, time.Now())
	if err != nil || !due {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	trim := exec.Command(executable, boundDefaultGoCacheFlag)
	trim.Dir = paths.ModuleDirectory
	if err := trim.Start(); err != nil {
		dispatch.Report.Fail("cohere: the default Go build cache was not bounded: %v", err)
		return
	}
	trim.Process.Release()
}

// boundDefaultGoCache is the copy's whole run: trim the default cache once and record any failure in the
// prune log, since nobody reads its output.
func boundDefaultGoCache() error {
	moduleDirectory, err := dispatch.FindModuleDirectory()
	if err != nil {
		return err
	}
	paths := dispatch.DefaultPaths(moduleDirectory)
	if _, err := dispatch.BoundDefaultGoCache(paths, time.Now()); err != nil {
		if log, openErr := os.OpenFile(paths.PruneLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); openErr == nil {
			fmt.Fprintf(log, "%s the default Go build cache was not bounded: %v\n", time.Now().Format(time.RFC3339), err)
			log.Close()
		}
		return err
	}
	return nil
}
