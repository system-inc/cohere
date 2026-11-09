package bench_results

import (
	"strings"
	"testing"
)

// guardRecord is a record that validates, of one project on one machine, measuring Cold over three rounds
// whose verdicts all take medianSeconds. A quiet record's runs are idle on both sides of the floor and a
// loaded one's are not, so a loaded record has no quiet summary, as a real loaded night has none.
func guardRecord(project string, model string, medianSeconds float64, quiet bool) Record {
	idle := 95.0
	if !quiet {
		idle = 40
	}
	runs := []Run{}
	for round := 1; round <= 3; round++ {
		runs = append(runs, Run{
			Mode:           ModeCold,
			Round:          round,
			VerdictSeconds: medianSeconds,
			SettledSeconds: medianSeconds,
			EngineSeconds:  medianSeconds,
			IdleBefore:     idle,
			IdleAfter:      idle,
			Quiet:          quiet,
			Cache:          "off",
		})
	}
	return Record{
		Schema:     SchemaVersion,
		RecordedAt: "2026-11-09T03:00:00Z",
		Cohere:     Cohere{Commit: strings.Repeat("a", 40), Version: "cohere 1.0.0"},
		Project:    Project{Name: project, Commit: strings.Repeat("b", 40)},
		Machine: Machine{
			Model:       model,
			Processor:   "Apple M4 Max",
			Cores:       16,
			MemoryBytes: 1 << 36,
			Os:          "macOS 27.2",
		},
		IdleFloor:     90,
		SettleSeconds: 60,
		Rounds:        3,
		MeasuredModes: []Mode{ModeCold},
		Runs:          runs,
		Modes:         Summarize([]Mode{ModeCold}, runs),
	}
}

// The helper's records are records a reader accepts, so a verdict on them is a verdict on what the bench writes.
func TestGuardRecordValidates(test *testing.T) {
	test.Parallel()
	for _, quiet := range []bool{true, false} {
		if validateError := Validate(guardRecord("ahra", "Mac16,5", 10, quiet)); validateError != nil {
			test.Errorf("a record quiet %v does not validate: %v", quiet, validateError)
		}
	}
}

// The threshold is a bound, not a band to land in: a median just inside it passes either way, and one just
// past it is called, slower or faster. Each case sits a tenth of a percent from the 10% line, so a comparison
// that moved the line, or let it include its own edge loosely, turns one of them.
func TestGuardJudgesTonightAgainstTheThreshold(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]struct {
		tonightSeconds float64
		verdict        GuardVerdict
	}{
		"9.9% slower passes":     {tonightSeconds: 10.99, verdict: GuardVerdictPass},
		"10.1% slower regressed": {tonightSeconds: 11.01, verdict: GuardVerdictRegressed},
		"9.9% faster passes":     {tonightSeconds: 9.01, verdict: GuardVerdictPass},
		"10.1% faster is faster": {tonightSeconds: 8.99, verdict: GuardVerdictFaster},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			baseline := guardRecord("ahra", "Mac16,5", 10, true)
			tonight := guardRecord("ahra", "Mac16,5", want.tonightSeconds, true)
			tonight.Cohere.Commit = strings.Repeat("c", 40)
			result, guardError := Guard(baseline, tonight, 0.10)
			if guardError != nil {
				test.Fatal(guardError)
			}
			if result.Verdict != want.verdict {
				test.Fatalf("ratio %g read %s, want %s", result.Ratio, result.Verdict, want.verdict)
			}
			// The commits bound the change, so a regression names both engines it lies between.
			if result.BaselineCohereCommit != baseline.Cohere.Commit ||
				result.TonightCohereCommit != tonight.Cohere.Commit {
				test.Errorf("the result names cohere %s to %s", result.BaselineCohereCommit, result.TonightCohereCommit)
			}
			if result.BaselineMedianSeconds != 10 || result.TonightMedianSeconds != want.tonightSeconds {
				test.Errorf(
					"the result reads medians %g and %g",
					result.BaselineMedianSeconds,
					result.TonightMedianSeconds,
				)
			}
		})
	}
}

// A night with no number is Unmeasured, never a pass and never an error: the machine was never quiet, or the
// engine came from a modified tree. The reason names which, so the morning reader knows what to fix.
func TestGuardCallsANightWithNoNumberUnmeasured(test *testing.T) {
	test.Parallel()
	loaded := guardRecord("ahra", "Mac16,5", 30, false)
	dirty := guardRecord("ahra", "Mac16,5", 10, true)
	dirty.Cohere.Dirty = true
	for name, want := range map[string]struct {
		tonight Record
		reason  string
	}{
		"no quiet run": {tonight: loaded, reason: "no Cold run tonight was quiet"},
		"dirty engine": {tonight: dirty, reason: "built from a modified tree"},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			result, guardError := Guard(guardRecord("ahra", "Mac16,5", 10, true), want.tonight, 0.10)
			if guardError != nil {
				test.Fatal(guardError)
			}
			if result.Verdict != GuardVerdictUnmeasured {
				test.Fatalf("read %s, want %s", result.Verdict, GuardVerdictUnmeasured)
			}
			if !strings.Contains(result.Reason, want.reason) {
				test.Errorf("the reason %q does not say %q", result.Reason, want.reason)
			}
			if result.Ratio != 0 || result.TonightMedianSeconds != 0 {
				test.Errorf(
					"an unmeasured night reads ratio %g and median %g",
					result.Ratio,
					result.TonightMedianSeconds,
				)
			}
		})
	}
}

// Numbers are compared only like for like, and only against a quiet baseline: across projects or machines,
// or against a baseline that never had a quiet number, the guard refuses rather than reaching a verdict.
func TestGuardRefusesWhatIsNotComparable(test *testing.T) {
	test.Parallel()
	otherCores := guardRecord("ahra", "Mac16,5", 10, true)
	otherCores.Machine.Cores = 12
	for name, want := range map[string]struct {
		baseline Record
		tonight  Record
		refusal  string
	}{
		"different project": {
			baseline: guardRecord("ahra", "Mac16,5", 10, true),
			tonight:  guardRecord("cohere", "Mac16,5", 10, true),
			refusal:  "different projects are never compared",
		},
		"different model": {
			baseline: guardRecord("ahra", "Mac16,5", 10, true),
			tonight:  guardRecord("ahra", "Mac15,9", 10, true),
			refusal:  "different machines are never compared",
		},
		"different cores": {
			baseline: guardRecord("ahra", "Mac16,5", 10, true),
			tonight:  otherCores,
			refusal:  "different machines are never compared",
		},
		"loaded baseline": {
			baseline: guardRecord("ahra", "Mac16,5", 10, false),
			tonight:  guardRecord("ahra", "Mac16,5", 10, true),
			refusal:  "a baseline must be a quiet number",
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			result, guardError := Guard(want.baseline, want.tonight, 0.10)
			if guardError == nil {
				test.Fatalf("reached %+v", result)
			}
			if !strings.Contains(guardError.Error(), want.refusal) {
				test.Errorf("the refusal %q does not say %q", guardError, want.refusal)
			}
		})
	}
}

// The streak counts only the newest nights: one measured night ends it, however many came before.
func TestUnmeasuredStreakCountsTheNewestNights(test *testing.T) {
	test.Parallel()
	for _, want := range []struct {
		verdicts []GuardVerdict
		streak   int
	}{
		{
			verdicts: []GuardVerdict{
				GuardVerdictPass,
				GuardVerdictUnmeasured,
				GuardVerdictUnmeasured,
				GuardVerdictUnmeasured,
			},
			streak: 3,
		},
		{verdicts: []GuardVerdict{GuardVerdictUnmeasured, GuardVerdictPass}, streak: 0},
		{verdicts: nil, streak: 0},
	} {
		if streak := UnmeasuredStreak(want.verdicts); streak != want.streak {
			test.Errorf("%v read a streak of %d, want %d", want.verdicts, streak, want.streak)
		}
	}
}
