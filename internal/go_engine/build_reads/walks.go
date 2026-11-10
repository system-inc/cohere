package build_reads

import (
	"sync"

	"golang.org/x/tools/go/packages"
)

// Walks are the type-read walks a run leaves running beside it, each on a goroutine of its own, at most concurrency
// at once, so the walk of the run's largest package never sits at the end of its chain. A reader of the reads waits
// for them first (Wait); a run nothing reads the reads of never waits, so its verdict never pays for them.
type Walks struct {
	slots chan struct{}
	group sync.WaitGroup

	// hold, nil outside tests, is called with each walk's package once it holds its slot and before it runs, so a test
	// can keep a walk from ending until it has read what a reader would read early.
	hold func(owner *packages.Package)

	// waiting, nil outside tests, is called as Wait starts waiting, so a test can let a held walk end only once a
	// reader is waiting for it.
	waiting func()
}

// NewWalks is walks run at most concurrency at once. hold and waiting, nil but in tests, are called before each walk
// and as Wait starts waiting (see Walks.hold and Walks.waiting).
func NewWalks(concurrency int, hold func(owner *packages.Package), waiting func()) *Walks {
	return &Walks{slots: make(chan struct{}, concurrency), hold: hold, waiting: waiting}
}

// Go runs walk, the walk of owner's type reads and whatever must follow it, on a goroutine of its own, once ready is
// closed when it is not nil, and once a slot is free. Wait waits for it from the moment Go returns.
func (walks *Walks) Go(owner *packages.Package, ready <-chan struct{}, walk func()) {
	walks.group.Go(func() {
		if ready != nil {
			<-ready
		}
		walks.slots <- struct{}{}
		defer func() { <-walks.slots }()
		if walks.hold != nil {
			walks.hold(owner)
		}
		walk()
	})
}

// Wait returns once every walk Go started has ended, at once for nil walks, a run that started none.
func (walks *Walks) Wait() {
	if walks == nil {
		return
	}
	if walks.waiting != nil {
		walks.waiting()
	}
	walks.group.Wait()
}
