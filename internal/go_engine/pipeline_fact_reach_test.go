package go_engine

import (
	"path/filepath"
	"slices"
	"testing"
)

// Under the body reach census, with `--other-builds --build-dependence`, the host's lint hands back what its run held
// of its facts for the facts' reach (#aswfm7a), the body-reach fixture's one package among it, and a run without the
// census asks for none, its findings the same as the census's.
func TestTheHostsLintCapturesItsFactsUnderTheBodyReachCensus(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach"))
	on, onStream := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
	}, "--no-cache", "--no-fix", "--other-builds", "--build-dependence")
	off, offStream := runOver(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings,
		"--no-fix", "--other-builds", "--build-dependence")

	if len(on.buildRuns) != 2 || len(off.buildRuns) != 2 {
		test.Fatalf("want the host's build and windows's in both runs, got %d and %d runs", len(on.buildRuns),
			len(off.buildRuns))
	}
	captured := on.buildRuns[0].result.FactReach
	if captured == nil || captured.Packages["example.com/reach"] == nil {
		test.Errorf("under the census the host's lint captured %+v, want example.com/reach among it", captured)
	}
	if off.buildRuns[0].result.FactReach != nil {
		test.Error("a run without the census captured the host's facts")
	}
	if !slices.Equal(findingLines(normalize(onStream, root)), findingLines(offStream)) {
		test.Errorf("the findings with the census are\n%q\nwant those without it\n%q",
			findingLines(normalize(onStream, root)), findingLines(offStream))
	}
}
