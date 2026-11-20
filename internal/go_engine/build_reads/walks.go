package build_reads

import (
	"sync"

	"golang.org/x/tools/go/packages"
)

// Walks are the type-read walks a run leaves running beside it, each on a goroutine of its own and in a slot of its
// slots, so the walk of the run's largest package never sits at the end of its chain. A reader of the reads waits for
// them first (Wait); a run nothing reads the reads of never waits, so its verdict never pays for them.
type Walks struct {
	slots Slots
	group sync.WaitGroup

	// hold, nil outside tests, is called with each walk's package once it holds its slot and before it runs, so a test
	// can keep a walk from ending until it has read what a reader would read early.
	hold func(owner *packages.Package)

	// waiting, nil outside tests, is called as Wait starts waiting, so a test can let a held walk end only once a
	// reader is waiting for it.
	waiting func()

	// ended is each owner's channel, made as Go starts its walk and closed once the walk has ended and given its slot
	// back (Ended), guarded by endedLock.
	endedLock sync.Mutex
	ended     map[*packages.Package]chan struct{}
}

// endedAlready is the channel Ended gives for an owner with no walk, closed from the start.
var endedAlready = func() chan struct{} {
	closed := make(chan struct{})
	close(closed)
	return closed
}()

// Slots are what a walk takes one of as it starts and gives back as it ends, as the load's are (load.WalkSlots), taken
// below every package's work, so a walk never displaces the load.
type Slots interface {
	Acquire()
	Release()
}

// NewWalks is walks each run in a slot of slots, or of slots of their own, concurrency of them, when slots is nil.
// hold and waiting, nil but in tests, are called before each walk and as Wait starts waiting (see Walks.hold and
// Walks.waiting).
func NewWalks(concurrency int, slots Slots, hold func(owner *packages.Package), waiting func()) *Walks {
	walks := &Walks{slots: slots, hold: hold, waiting: waiting}
	if slots == nil {
		walks.slots = channelSlots(make(chan struct{}, concurrency))
	}
	return walks
}

// Go runs walk, the walk of owner's type reads and whatever must follow it, on a goroutine of its own, once ready is
// closed when it is not nil, and once a slot is free. Wait waits for it from the moment Go returns.
func (walks *Walks) Go(owner *packages.Package, ready <-chan struct{}, walk func()) {
	// Made before the walk's goroutine starts, so an Ended asked once Go returns waits for this walk.
	ended := make(chan struct{})
	walks.endedLock.Lock()
	if walks.ended == nil {
		walks.ended = map[*packages.Package]chan struct{}{}
	}
	walks.ended[owner] = ended
	walks.endedLock.Unlock()
	walks.group.Go(func() {
		// Deferred first, so it runs last: a reader waiting for the walk takes its own slot once this one is free.
		defer close(ended)
		if ready != nil {
			<-ready
		}
		walks.slots.Acquire()
		defer walks.slots.Release()
		if walks.hold != nil {
			walks.hold(owner)
		}
		walk()
	})
}

// Ended is closed once owner's walk has ended, its reads recorded, so a reader of one package's reads waits for its
// walk alone rather than every walk (Wait). It is closed already for nil walks, and for an owner Go started no walk of,
// whose reads are as recorded as they will be from Go's side; a reader asking before Go was called for owner gets
// that closed channel too. Each owner's walk is started once, since its reads are claimed once (Reads.Claim).
func (walks *Walks) Ended(owner *packages.Package) <-chan struct{} {
	if walks == nil {
		return endedAlready
	}
	walks.endedLock.Lock()
	defer walks.endedLock.Unlock()
	if ended, started := walks.ended[owner]; started {
		return ended
	}
	return endedAlready
}

// Started reports whether Go started a walk of owner, false for nil walks, so a caller walking whatever no walk reached
// starts none twice (check's recordUnreachedTypes).
func (walks *Walks) Started(owner *packages.Package) bool {
	if walks == nil {
		return false
	}
	walks.endedLock.Lock()
	defer walks.endedLock.Unlock()
	_, started := walks.ended[owner]
	return started
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
