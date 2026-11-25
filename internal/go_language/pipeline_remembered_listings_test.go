package go_language

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/load"
)

// A build's load after its first check takes the listing its first load made (#32594zm): over the fixture whose
// windows build declines two packages at its check and loads once more (TestATimelineTellsABuildsReloadByItsRound),
// under `--no-cache`, the host's load and windows's first each list and remember what they listed, and windows's
// second is answered from what its first remembered, listing nothing. Every listing here is the in-process lister's,
// which starts no go command whether it is answered or not, so it is the run's fingerprints that count it. The run's
// stream is the one the same run writes with nothing remembered, its fingerprints taken away.
func TestABuildsReloadTakesItsFirstListing(test *testing.T) {
	test.Parallel()
	rememberingRoot := copyFixture(test, filepath.Join("testdata", "restore-from-host"))
	remembering, rememberingStream := runOverConfigured(test, rememberingRoot, exitFindings, nil, "--no-cache",
		"--no-fix")
	windows := windowsRun(test, remembering)
	if len(windows.checkedFromSourceRounds) != 2 {
		test.Fatalf("windows loaded %d times, want twice: its first load, and once more for what its check declined",
			len(windows.checkedFromSourceRounds))
	}
	remembered, answered := remembering.fingerprints.ListingsRemembered()
	if remembered != int64(len(remembering.buildRuns)) || answered != 1 {
		test.Errorf("over %d builds, %d listings were remembered and %d asks answered, want one remembered for each "+
			"build's first load and windows's second answered", len(remembering.buildRuns), remembered, answered)
	}

	forgettingRoot := copyFixture(test, filepath.Join("testdata", "restore-from-host"))
	forgetting, forgettingStream := runOverConfigured(test, forgettingRoot, exitFindings, func(engine *pipeline) {
		// With no fingerprints the run's loads remember no listing, and under `--no-cache` the fingerprints read
		// nothing else: no store keeps a listing for one to be read against.
		engine.fingerprints = nil
	}, "--no-cache", "--no-fix")
	if forgetting.fingerprints != nil {
		test.Fatal("the run with nothing remembered started its fingerprints over, so it remembered after all")
	}
	if rememberingNormalized, forgettingNormalized := normalize(rememberingStream, rememberingRoot),
		normalize(forgettingStream, forgettingRoot); rememberingNormalized != forgettingNormalized {
		test.Errorf("the run's stream with its listings remembered differs from the one with none:\n%s\n\nwant\n%s",
			rememberingNormalized, forgettingNormalized)
	}
}

// A listing made before the run's held bytes changed is never answered after the change (#32594zm): over the fixture
// whose long lines the wrap rewrites, under `--no-cache`, the host's first load lists and remembers under the run's
// first fingerprints, the wrap writes and the run holds what it wrote (heldChanged), and the host's load of the wrapped
// tree, the same workspace, patterns and go environment, lists again under fingerprints of their own, answered from
// nothing remembered before the write.
func TestAListingBeforeAWriteIsNeverAnsweredAfterIt(test *testing.T) {
	test.Parallel()
	var first *load.Fingerprints
	engine, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "long-lines")), exitFindings,
		func(engine *pipeline) { first = engine.fingerprints }, "--no-cache")
	if loads := engine.hostLoads.Load(); loads != 2 {
		test.Fatalf("the host loaded %d times, want twice: before the wrap wrote and after", loads)
	}
	if engine.fingerprints == first {
		test.Fatal("the wrap's write left the run's fingerprints as they were")
	}
	if remembered, answered := first.ListingsRemembered(); remembered != 1 || answered != 0 {
		test.Errorf("before the write, %d listings were remembered and %d asks answered, want the host's first "+
			"load's one remembered and none answered", remembered, answered)
	}
	if remembered, answered := engine.fingerprints.ListingsRemembered(); remembered != 1 || answered != 0 {
		test.Errorf("after the write, %d listings were remembered and %d asks answered, want the host's load of the "+
			"wrapped tree's one remembered, listed again, and none answered", remembered, answered)
	}
}
