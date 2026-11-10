package go_engine

import (
	"go/types"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
)

// The extra builds read the host's reads whole: a walk the host's run left running is held until wholeReads starts
// waiting for it, or until wholeReads returns, whichever comes first, so a read that skipped the wait finds the
// package unknown every time, and one that waits finds it recorded.
func TestTheExtraBuildsReadTheHostsReadsWhole(test *testing.T) {
	test.Parallel()

	walked := &packages.Package{ID: "example.com/walked", Types: types.NewPackage("example.com/walked", "walked")}
	reads := &build_reads.Reads{}
	hold := make(chan struct{})
	var released sync.Once
	release := func() { released.Do(func() { close(hold) }) }
	walks := build_reads.NewWalks(1, nil, nil, release)
	walks.Go(walked, nil, func() {
		<-hold
		reads.RecordTypes(walked.Types, &types.Info{})
	})
	whole := wholeReads(&check.Result{Reads: map[*packages.Package]*build_reads.Reads{walked: reads}, ReadWalks: walks})
	restorable, why := whole[walked].Restorable()
	release()
	walks.Wait()
	if !restorable {
		test.Fatalf("the extra builds read the host's reads before its walk ended: %s", why)
	}
}
