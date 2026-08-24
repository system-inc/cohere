package reactconformancescore

import (
	"testing"

	"github.com/system-inc/verify/internal/reactconformance"
)

// TestWiredRulesAgainstReactsOwnGoldens is the measurement: every wired rule over every fixture
// attributed to it, categorised, with each failure named.
//
// It asserts nothing about the totals yet; it is the instrument that produces the numbers the
// pinned assertions are then written from. A run that named no fixtures would be the tell that the
// selection is empty rather than the rules being perfect.
func TestWiredRulesAgainstReactsOwnGoldens(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a TypeScript program per fixture")
	}
	fixtures, err := reactconformance.Load(fixtureRoot)
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}

	grand := map[reactconformance.Verdict]int{}
	for _, upstream := range UpstreamNames() {
		subject := Rules[upstream]
		scored := SelectFixtures(fixtures, upstream)
		counts := map[reactconformance.Verdict]int{}
		var failures []string
		for _, fixture := range scored {
			result, analyzeErr := Analyze(subject, fixture, t.TempDir())
			verdict := reactconformance.Classify(fixture, result, analyzeErr)
			counts[verdict.Verdict]++
			grand[verdict.Verdict]++
			if verdict.Verdict == reactconformance.VerdictFailed {
				failures = append(failures, "      "+fixture.Name+" :: "+verdict.Reason)
			}
		}
		t.Logf("%-20s selected %3d  passed %3d  failed %3d  divergence %2d  notscored %2d  unresolvable %2d",
			upstream, len(scored),
			counts[reactconformance.VerdictPassed],
			counts[reactconformance.VerdictFailed],
			counts[reactconformance.VerdictStatedDivergence],
			counts[reactconformance.VerdictNotScored],
			counts[reactconformance.VerdictUnresolvableTypes])
		for _, f := range failures {
			t.Log(f)
		}
	}
	t.Logf("TOTAL passed %d failed %d divergence %d notscored %d unresolvable %d",
		grand[reactconformance.VerdictPassed], grand[reactconformance.VerdictFailed],
		grand[reactconformance.VerdictStatedDivergence], grand[reactconformance.VerdictNotScored],
		grand[reactconformance.VerdictUnresolvableTypes])
}
