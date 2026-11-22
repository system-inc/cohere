package go_engine

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// The host's go env is whole once the description hands it out (#e8w514b): describeProject reads it from the early
// start (earlyStart.hostGoEnvironment) only once the early start has done writing it, its build context's kept place
// among it (load.GoEnvironment.KeepContext), so a reader of it needs no await. Over the other-builds fixture, with a
// system cache of the test's own, whose store keeps the build context, and the types phase alone, so the description
// reads no builds and returns as soon as the go env is handed out, the test fetches the host's lister state straight
// after, the read of the kept place a lister makes, and only then awaits what the description started. Under `-race` a
// description that hands the go env out first is a data race.
func TestTheHostsGoEnvironmentIsWholeOnceTheDescriptionHandsItOut(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
		"--no-cache", "--types"})
	if parseError != nil {
		test.Fatal(parseError)
	}
	// A system cache of the test's own, so the early start predicts where the build context is kept.
	request.standardLibrary.systemCacheRoot = test.TempDir()
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	// What the description started beside its reads ends before the test and its directory do.
	test.Cleanup(described.awaitStarted)
	if described.goEnvironment == nil {
		test.Fatal("the description handed out no go env")
	}
	described.goEnvironment.FetchListerState(nil)
}
