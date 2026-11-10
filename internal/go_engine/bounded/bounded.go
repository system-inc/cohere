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
// set order.
func ForEach(count int, singleThreaded bool, work func(index int)) {
	if singleThreaded {
		for index := range count {
			work(index)
		}
		return
	}
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	var group sync.WaitGroup
	for index := range count {
		slots <- struct{}{}
		group.Go(func() {
			defer func() { <-slots }()
			work(index)
		})
	}
	group.Wait()
}
