package go_engine

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// earlyFetchHoldDeadline bounds the early start's hold on its own fetch, so a test that never lets it go, one that
// failed before its fetch, fails rather than never ending. Nothing is ordered by it: the hold ends on the test's fetch.
const earlyFetchHoldDeadline = 2 * time.Minute

// The host's go env is whole once the description hands it out (#e8w514b): describeProject reads it from the early
// start (earlyStart.hostGoEnvironment) only once the early start has done writing it, its build context's kept place
// among it (load.GoEnvironment.KeepContext), so a reader of it needs no await. Over the other-builds fixture, with a
// system cache of the test's own, whose store keeps the build context, and the types phase alone, so the description
// reads no builds and returns as soon as the go env is handed out, the test fetches the host's lister state straight
// after, the read of the kept place a lister makes, and only then awaits what the description started. The early start
// holds its own fetch until the test's has run (hostFetchHeld), so the test's is the first through the lister state's
// once and reads the kept place itself on every run, never after a fetch of the early start's that would order it.
// Under `-race` a description that hands the go env out first is a data race, at -count=1.
func TestTheHostsGoEnvironmentIsWholeWhenALoadReadsItAtOnceWithTheEarlyStart(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
		"--no-cache", "--types"})
	if parseError != nil {
		test.Fatal(parseError)
	}
	// A system cache of the test's own, so the early start predicts where the build context is kept.
	request.standardLibrary.systemCacheRoot = test.TempDir()
	fetched := make(chan struct{})
	var letGo sync.Once
	release := func() { letGo.Do(func() { close(fetched) }) }
	request.hostFetchHeld = func() {
		select {
		case <-fetched:
		case <-time.After(earlyFetchHoldDeadline):
			test.Errorf("the early start's fetch was held %v for the test's, which never ran", earlyFetchHoldDeadline)
		}
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	// What the description started beside its reads ends before the test and its directory do, the hold let go
	// first, since cleanups run last registered first, so a test that fails before its fetch never waits it out.
	test.Cleanup(described.awaitStarted)
	test.Cleanup(release)
	if described.goEnvironment == nil {
		test.Fatal("the description handed out no go env")
	}
	described.goEnvironment.FetchListerState(nil)
	release()
}
