package build_reads

import (
	"testing"

	"golang.org/x/tools/go/packages"
)

// A walk's Ended is closed only once that walk has ended, whatever the other walks do, and at once for an owner with
// no walk or for nil walks (#ybjbqm2): with walked's walk held, asked as Go returns, its Ended is open, other's, whose
// walk ran, closes, and once walked's walk is let go, its Ended closes after the walk recorded.
func TestAWalksEndedClosesOnceThatWalkHasEnded(test *testing.T) {
	test.Parallel()

	walked, other, unwalked := &packages.Package{ID: "walked"}, &packages.Package{ID: "other"},
		&packages.Package{ID: "unwalked"}
	hold := make(chan struct{})
	walks := NewWalks(2, nil, func(owner *packages.Package) {
		if owner == walked {
			<-hold
		}
	}, nil)
	recorded := false
	walks.Go(walked, nil, func() { recorded = true })
	walks.Go(other, nil, func() {
		// Nothing to walk: other's walk only has to end.
	})
	walkedEnded := walks.Ended(walked)
	<-walks.Ended(other)
	select {
	case <-walkedEnded:
		test.Errorf("walked's Ended closed while its walk was held")
	default:
	}
	close(hold)
	<-walkedEnded
	// Read after Ended closed: the close is after the walk, so this read is ordered after its write.
	if !recorded {
		test.Errorf("walked's Ended closed before its walk ran")
	}
	select {
	case <-walks.Ended(unwalked):
	default:
		test.Errorf("Ended of an owner with no walk is open, want it closed")
	}
	var none *Walks
	select {
	case <-none.Ended(walked):
	default:
		test.Errorf("Ended of nil walks is open, want it closed")
	}
	walks.Wait()
}
