// Package bounded runs a piece of work for each index of a list, as many at once as there are cores, so a phase that
// reads or formats every file of a run fans out the same way wherever it runs: gofmt's check and the long-line wrap
// (format), and the project description's read of every file of ours.
package bounded

import (
	"runtime"
	"sync"
)

// ForEach runs work for every index below count, as many at once as there are cores unless singleThreaded, and
// returns once every one has. work must keep what it makes in its own index's place, since the indexes finish in no
// set order. A panic in work reaches the caller (panicCarrier): every other index still runs, and the first panic
// recovered is raised again once they have.
func ForEach(count int, singleThreaded bool, work func(index int)) {
	if singleThreaded {
		// work runs on the caller's goroutine here, so a panic in it stops the loop and reaches the caller as it is,
		// with nothing to carry back (#z8ym3x5).
		for index := range count {
			work(index)
		}
		return
	}
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	var carrier panicCarrier
	var group sync.WaitGroup
	for index := range count {
		slots <- struct{}{}
		group.Go(func() {
			defer func() { <-slots }()
			carrier.run(func() { work(index) })
		})
	}
	group.Wait()
	carrier.raise()
}
