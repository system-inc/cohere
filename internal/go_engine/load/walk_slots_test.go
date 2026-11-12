package load

import (
	"runtime"
	"slices"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
)

// With the load's one slot held by a package's work, a walk that began waiting first and a package's read that began
// waiting after it are granted the slot the package first: a walk never displaces the load.
func TestAWalkIsGrantedALoadSlotOnlyAfterEveryPackagesWork(test *testing.T) {
	test.Parallel()
	slots := bounded.NewPool(1)
	walkSlots := &WalkSlots{slots: slots}
	slots.Acquire(4)
	var grantedLock sync.Mutex
	var granted []string
	var group sync.WaitGroup
	take := func(name string, acquire func(), release func()) {
		group.Go(func() {
			acquire()
			grantedLock.Lock()
			granted = append(granted, name)
			grantedLock.Unlock()
			release()
		})
	}
	take("walk", walkSlots.Acquire, walkSlots.Release)
	// The walk waits before the package's read starts, so it arrived first.
	for slots.Waiters() < 1 {
		runtime.Gosched()
	}
	take("read", func() { slots.Acquire(1) }, slots.Release)
	for slots.Waiters() < 2 {
		runtime.Gosched()
	}
	slots.Release()
	group.Wait()
	if want := []string{"read", "walk"}; !slices.Equal(granted, want) {
		test.Errorf("the slot was granted to %v, want %v", granted, want)
	}
}
