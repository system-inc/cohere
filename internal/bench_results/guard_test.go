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

// A record whose runs asked for the host's build alone checked one build, so one that names more is refused. An
// every-build record that checked one is not, since a tree whose files name no other platform has no other build.
func TestARecordOfTheHostsBuildAloneNamesOneBuild(test *testing.T) {
	test.Parallel()
	hostAlone := guardRecord("cohere", "Mac16,5", 2, true)
	hostAlone.Builds = []string{"darwin/arm64 cgo", "linux/arm64"}
	validateError := Validate(hostAlone)
	if validateError == nil || !strings.Contains(validateError.Error(), "asked for the host's build alone") {
		test.Errorf("a host-only record of two builds validates with %v, want refused", validateError)
	}
	oneEveryBuild := guardRecord("cohere", "Mac16,5", 2, true)
	oneEveryBuild.EveryBuild, oneEveryBuild.Builds = true, []string{"darwin/arm64 cgo"}
	if validateError := Validate(oneEveryBuild); validateError != nil {
		test.Errorf("an every-build record of one build is refused: %v", validateError)
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
	goAlone := guardRecord("cohere", "Mac16,5", 1.9, true)
	goAlone.Engine = EngineGo
	goAloneChecking := goAlone
	goAloneChecking.NoFix = true
	everyBuild := goAloneChecking
	everyBuild.EveryBuild = true
	everyBuild.Builds = []string{"darwin/arm64 cgo", "linux/arm64", "windows/arm64"}
	everyBuildHostAlone := everyBuild
	everyBuildHostAlone.Builds = []string{"darwin/arm64 cgo"}
	release := goAloneChecking
	release.StandardLibraryTable = &StandardLibraryTable{ReleaseStamp: "v0.2.0+e51c6a1e", Bytes: 1 << 20}
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
		// A narrowed run is never the whole root's baseline, nor judged against it, either way round.
		"a Go project alone against the whole root": {
			baseline: guardRecord("cohere", "Mac16,5", 10, true),
			tonight:  goAlone,
			refusal:  "checked the whole root and tonight checked the Go project alone",
		},
		"the whole root against a Go project alone": {
			baseline: goAlone,
			tonight:  guardRecord("cohere", "Mac16,5", 10, true),
			refusal:  "checked the Go project alone and tonight checked the whole root",
		},
		// A checking run is never judged against a fixing one, either way round, on the same project and engine.
		"a checking night against a fixing baseline": {
			baseline: goAlone,
			tonight:  goAloneChecking,
			refusal:  "the baseline's cold runs fixed and tonight's checked without fixing",
		},
		"a fixing night against a checking baseline": {
			baseline: goAloneChecking,
			tonight:  goAlone,
			refusal:  "the baseline's cold runs checked without fixing and tonight's fixed",
		},
		// An every-build night is never judged against a host-only baseline, either way round (#ccqee0e).
		"an every-build night against a host-only baseline": {
			baseline: goAloneChecking,
			tonight:  everyBuild,
			refusal:  "the baseline checked the host's build alone and tonight every build the Go files name",
		},
		"a host-only night against an every-build baseline": {
			baseline: everyBuild,
			tonight:  goAloneChecking,
			refusal:  "the baseline checked every build the Go files name and tonight the host's build alone",
		},
		// A night labelled every-build that checked the host's alone, its forwarding broken, is never judged against
		// an every-build baseline, though the labels agree: the builds it read decide.
		"an every-build night that checked the host's alone": {
			baseline: everyBuild,
			tonight:  everyBuildHostAlone,
			refusal: "the baseline checked the builds darwin/arm64 cgo; linux/arm64; windows/arm64 and tonight " +
				"darwin/arm64 cgo, and runs of different builds are never compared",
		},
		// A release binary's night is never judged against a local build's baseline, either way round (#ccqee0e).
		"a release night against a local build's baseline": {
			baseline: goAloneChecking,
			tonight:  release,
			refusal: "the baseline's cohere carried no standard library table (a local build) and tonight's " +
				"carried release v0.2.0+e51c6a1e's standard library table",
		},
		"a local build's night against a release baseline": {
			baseline: release,
			tonight:  goAloneChecking,
			refusal:  "a local build is never compared with a release's",
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

// Two runs narrowed to the same engine compare as two whole-root runs do: the engine bounds what is
// compared, and changes nothing else about the verdict.
func TestGuardComparesRunsOfTheSameEngine(test *testing.T) {
	test.Parallel()
	baseline := guardRecord("cohere", "Mac16,5", 2, true)
	baseline.Engine = EngineGo
	tonight := guardRecord("cohere", "Mac16,5", 2.5, true)
	tonight.Engine = EngineGo
	result, guardError := Guard(baseline, tonight, 0.10)
	if guardError != nil {
		test.Fatal(guardError)
	}
	if result.Verdict != GuardVerdictRegressed || result.Ratio != 1.25 {
		test.Fatalf("read %s at ratio %g, want %s at 1.25", result.Verdict, result.Ratio, GuardVerdictRegressed)
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
