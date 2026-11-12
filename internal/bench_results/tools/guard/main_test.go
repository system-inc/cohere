package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/bench_results"
)

// writeRecord writes, under directory, a record that validates: one project measuring Cold over three rounds
// whose verdicts all take medianSeconds, quiet or loaded, as quiet_machine.sh's record tool would write it.
func writeRecord(test *testing.T, directory string, name string, medianSeconds float64, quiet bool) string {
	test.Helper()
	idle := 95.0
	if !quiet {
		idle = 40
	}
	runs := []bench_results.Run{}
	for round := 1; round <= 3; round++ {
		runs = append(runs, bench_results.Run{
			Mode:           bench_results.ModeCold,
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
	encoded, encodeError := bench_results.Encode(bench_results.Record{
		Schema:     bench_results.SchemaVersion,
		RecordedAt: "2026-11-09T03:00:00Z",
		Cohere:     bench_results.Cohere{Commit: strings.Repeat("a", 40), Version: "cohere 1.0.0"},
		Project:    bench_results.Project{Name: "ahra", Commit: strings.Repeat("b", 40)},
		Machine: bench_results.Machine{
			Model:       "Mac16,5",
			Processor:   "Apple M4 Max",
			Cores:       16,
			MemoryBytes: 1 << 36,
			Os:          "macOS 27.2",
		},
		IdleFloor:     90,
		SettleSeconds: 60,
		Rounds:        3,
		MeasuredModes: []bench_results.Mode{bench_results.ModeCold},
		Runs:          runs,
		Modes:         bench_results.Summarize([]bench_results.Mode{bench_results.ModeCold}, runs),
	})
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	path := filepath.Join(directory, name)
	if writeError := os.WriteFile(path, encoded, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return path
}

// runGuard runs the command in process and gives its exit, what it printed, and what it said went wrong.
func runGuard(arguments ...string) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := run(arguments, &stdout, &stderr)
	return exit, stdout.String(), stderr.String()
}

// lines reads a state file's lines, or none when it was never written.
func lines(test *testing.T, path string) []string {
	test.Helper()
	contents, readError := os.ReadFile(path)
	if os.IsNotExist(readError) {
		return nil
	}
	if readError != nil {
		test.Fatal(readError)
	}
	return strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
}

// A project's first quiet night becomes its baseline and passes, with the move on record, so the guard
// starts by itself rather than waiting for someone to choose a number.
func TestTheFirstQuietNightBecomesTheBaseline(test *testing.T) {
	test.Parallel()
	state := test.TempDir()
	tonightPath := writeRecord(test, test.TempDir(), "tonight.json", 10, true)
	exit, printed, said := runGuard("-state", state, "-tonight", tonightPath)
	if exit != 0 {
		test.Fatalf("exited %d:\n%s", exit, said)
	}
	var result printedResult
	if decodeError := json.Unmarshal([]byte(printed), &result); decodeError != nil {
		test.Fatalf("printed %q: %v", printed, decodeError)
	}
	if result.Verdict != bench_results.GuardVerdictPass || result.Reason != firstBaselineReason {
		test.Errorf("printed %+v", result)
	}
	tonight, readTonightError := os.ReadFile(tonightPath)
	if readTonightError != nil {
		test.Fatal(readTonightError)
	}
	baseline, readBaselineError := os.ReadFile(filepath.Join(state, "baseline-ahra.json"))
	if readBaselineError != nil {
		test.Fatal(readBaselineError)
	}
	if !bytes.Equal(baseline, tonight) {
		test.Errorf("the baseline is not tonight's record:\n%s", baseline)
	}
	if moves := lines(test, filepath.Join(state, "baselines-ahra.jsonl")); len(moves) != 1 ||
		!strings.Contains(moves[0], firstBaselineReason) {
		test.Errorf("the baseline moves on record are %q", moves)
	}
	if nights := lines(test, filepath.Join(state, "verdicts-ahra.jsonl")); len(nights) != 1 {
		test.Errorf("the verdicts on record are %q", nights)
	}
}

// A night more than the threshold slower exits 3 and is on record, against a baseline set by -accept. Two
// loaded nights after it are Unmeasured, exit 0, and count a streak of two, which the regressed night does not
// break into, since it is older than both.
func TestARegressedNightExitsThreeAndIsOnRecord(test *testing.T) {
	test.Parallel()
	state := test.TempDir()
	records := test.TempDir()
	baselinePath := writeRecord(test, records, "baseline.json", 10, true)
	exit, _, said := runGuard("-state", state, "-accept", baselinePath, "-because", "the measured start")
	if exit != 0 {
		test.Fatalf("accepting the baseline exited %d:\n%s", exit, said)
	}
	var printed string
	slowerPath := writeRecord(test, records, "slower.json", 12, true)
	exit, printed, said = runGuard("-state", state, "-tonight", slowerPath)
	if exit != regressedExit {
		test.Fatalf("a night 20%% slower exited %d, want %d:\n%s%s", exit, regressedExit, printed, said)
	}
	nights := lines(test, filepath.Join(state, "verdicts-ahra.jsonl"))
	var night verdictLine
	if len(nights) != 1 || json.Unmarshal([]byte(nights[0]), &night) != nil ||
		night.Verdict != bench_results.GuardVerdictRegressed || night.TonightMedianSeconds != 12 {
		test.Fatalf("the verdicts on record are %q", nights)
	}
	loadedPath := writeRecord(test, records, "loaded.json", 30, false)
	for want := 1; want <= 2; want++ {
		exit, printed, said = runGuard("-state", state, "-tonight", loadedPath)
		if exit != 0 {
			test.Fatalf("a loaded night exited %d:\n%s", exit, said)
		}
		var result printedResult
		if decodeError := json.Unmarshal([]byte(printed), &result); decodeError != nil {
			test.Fatalf("printed %q: %v", printed, decodeError)
		}
		if result.Verdict != bench_results.GuardVerdictUnmeasured || result.UnmeasuredStreak != want {
			test.Errorf("loaded night %d printed %+v", want, result)
		}
	}
}

// -accept moves the baseline only to a quiet number, and only with a reason: a loaded record or a missing
// -because exits 1 and leaves no baseline and no move on record.
func TestAcceptRefusesALoadedRecordAndAMissingReason(test *testing.T) {
	test.Parallel()
	records := test.TempDir()
	quietPath := writeRecord(test, records, "quiet.json", 10, true)
	loadedPath := writeRecord(test, records, "loaded.json", 30, false)
	for name, want := range map[string]struct {
		arguments []string
		refusal   string
	}{
		"loaded record": {
			arguments: []string{"-accept", loadedPath, "-because", "tonight"},
			refusal:   "a baseline must be a quiet number",
		},
		"missing because": {
			arguments: []string{"-accept", quietPath},
			refusal:   "-because is required",
		},
		"empty because": {
			arguments: []string{"-accept", quietPath, "-because", " "},
			refusal:   "-because is required",
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			state := test.TempDir()
			exit, _, said := runGuard(append([]string{"-state", state}, want.arguments...)...)
			if exit != 1 {
				test.Fatalf("exited %d, want 1:\n%s", exit, said)
			}
			if !strings.Contains(said, want.refusal) {
				test.Errorf("the refusal %q does not say %q", said, want.refusal)
			}
			for _, file := range []string{"baseline-ahra.json", "baselines-ahra.jsonl"} {
				if _, statError := os.Stat(filepath.Join(state, file)); !os.IsNotExist(statError) {
					test.Errorf("%s was written", file)
				}
			}
		})
	}
}

// writeNarrowedRecord writes the record writeRecord writes, narrowed to the Go project alone, as
// quiet_machine.sh's --engine Go records it.
func writeNarrowedRecord(test *testing.T, directory string, name string, medianSeconds float64) string {
	test.Helper()
	path := writeRecord(test, directory, name, medianSeconds, true)
	record, readError := readRecord(path)
	if readError != nil {
		test.Fatal(readError)
	}
	record.Engine = bench_results.EngineGo
	encoded, encodeError := bench_results.Encode(record)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if writeError := os.WriteFile(path, encoded, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return path
}

// A record narrowed to one engine's project never becomes a baseline, on either path that makes one: the first
// night and -accept each exit 1 naming the engine, and write no baseline, no move and no verdict. The same
// quiet record of the whole root is still made the baseline on both paths, so the refusal is the engine's alone.
func TestANarrowedRecordIsNeverABaseline(test *testing.T) {
	test.Parallel()
	records := test.TempDir()
	narrowedPath := writeNarrowedRecord(test, records, "narrowed.json", 10)
	wholePath := writeRecord(test, records, "whole.json", 10, true)
	for name, want := range map[string]struct {
		arguments func(recordPath string) []string
	}{
		"first night": {arguments: func(recordPath string) []string { return []string{"-tonight", recordPath} }},
		"accept": {arguments: func(recordPath string) []string {
			return []string{"-accept", recordPath, "-because", "the measured start"}
		}},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			state := test.TempDir()
			exit, _, said := runGuard(append([]string{"-state", state}, want.arguments(narrowedPath)...)...)
			if exit != 1 {
				test.Fatalf("a narrowed record exited %d, want 1:\n%s", exit, said)
			}
			if !strings.Contains(said, "checked the Go project alone (engine Go)") ||
				!strings.Contains(said, "a baseline is a record of the whole root") {
				test.Errorf("the refusal %q does not name the engine and the rule", said)
			}
			if entries, readError := os.ReadDir(state); readError != nil || len(entries) != 0 {
				test.Errorf("the state directory holds %v (%v)", entries, readError)
			}
			wholeState := test.TempDir()
			wholeArguments := append([]string{"-state", wholeState}, want.arguments(wholePath)...)
			if exit, _, said := runGuard(wholeArguments...); exit != 0 {
				test.Fatalf("a whole-root record exited %d:\n%s", exit, said)
			}
			if _, statError := os.Stat(filepath.Join(wholeState, "baseline-ahra.json")); statError != nil {
				test.Errorf("the whole-root record did not become the baseline: %v", statError)
			}
		})
	}
}

// A night that left no record still counts: after two loaded nights a lost one makes a streak of three, and
// after a measured night it starts one. The lost night carries its reason and the baseline's number, and is on
// record beside the others.
func TestALostNightCountsAsUnmeasured(test *testing.T) {
	test.Parallel()
	records := test.TempDir()
	quietPath := writeRecord(test, records, "quiet.json", 10, true)
	loadedPath := writeRecord(test, records, "loaded.json", 30, false)
	for name, want := range map[string]struct {
		before []string
		streak int
	}{
		"after two unmeasured nights": {before: []string{quietPath, loadedPath, loadedPath}, streak: 3},
		"after a pass":                {before: []string{quietPath}, streak: 1},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			state := test.TempDir()
			for _, tonightPath := range want.before {
				if exit, _, said := runGuard("-state", state, "-tonight", tonightPath); exit != 0 {
					test.Fatalf("judging %s exited %d:\n%s", tonightPath, exit, said)
				}
			}
			reason := "the bench exited 1; quiet_machine: the generator failed"
			exit, printed, said := runGuard("-state", state, "-project", "ahra", "-lost", reason)
			if exit != 0 {
				test.Fatalf("the lost night exited %d:\n%s", exit, said)
			}
			var result printedResult
			if decodeError := json.Unmarshal([]byte(printed), &result); decodeError != nil {
				test.Fatalf("printed %q: %v", printed, decodeError)
			}
			if result.Verdict != bench_results.GuardVerdictUnmeasured || result.UnmeasuredStreak != want.streak ||
				result.Reason != reason || result.Project != "ahra" || result.BaselineMedianSeconds != 10 {
				test.Errorf("printed %+v", result)
			}
			nights := lines(test, filepath.Join(state, "verdicts-ahra.jsonl"))
			if len(nights) != len(want.before)+1 || !strings.Contains(nights[len(nights)-1], "the generator failed") {
				test.Errorf("the verdicts on record are %q", nights)
			}
		})
	}
}

// A lost night names its project as a record would, so a name that would leave the state directory is
// refused, and -project without -lost, or the reverse, is a usage error.
func TestALostNightRefusesABadProject(test *testing.T) {
	test.Parallel()
	for name, arguments := range map[string][]string{
		"path in the name":   {"-project", "../ahra", "-lost", "crashed"},
		"project, no reason": {"-project", "ahra"},
		"reason, no project": {"-lost", "crashed"},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			state := test.TempDir()
			if exit, _, said := runGuard(append([]string{"-state", state}, arguments...)...); exit != 1 {
				test.Fatalf("exited %d, want 1:\n%s", exit, said)
			}
			if entries, readError := os.ReadDir(state); readError != nil || len(entries) != 0 {
				test.Errorf("the state directory holds %v (%v)", entries, readError)
			}
		})
	}
}

// writeCheckingRecord is writeRecord with its cold runs checking without fixing, as the nightly guard's are since
// #8sqsgee moved them to `--no-fix`.
func writeCheckingRecord(test *testing.T, directory string, name string, medianSeconds float64, quiet bool) string {
	test.Helper()
	path := writeRecord(test, directory, name, medianSeconds, quiet)
	record, readRecordError := readRecord(path)
	if readRecordError != nil {
		test.Fatal(readRecordError)
	}
	record.NoFix = true
	encoded, encodeError := bench_results.Encode(record)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if writeError := os.WriteFile(path, encoded, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return path
}

// The guard's cold runs moved from fixing to checking (#8sqsgee), and the two are never compared. A checking night
// against a fixing baseline is not an error that loses every night from then on: a loaded one is Unmeasured, saying the
// fixing baseline is never compared with it, and leaves the baseline alone; the first quiet one becomes the new
// baseline, the move on record with why. The other direction, a fixing night against a checking baseline, is no change
// the guard was told of, and is still refused.
func TestACheckingNightReplacesAFixingBaseline(test *testing.T) {
	test.Parallel()
	state := test.TempDir()
	records := test.TempDir()
	fixingPath := writeRecord(test, records, "fixing.json", 26, true)
	if exit, _, said := runGuard("-state", state, "-accept", fixingPath, "-because", "the fixing start"); exit != 0 {
		test.Fatalf("accepting the fixing baseline exited %d:\n%s", exit, said)
	}
	baselinePath := filepath.Join(state, "baseline-ahra.json")
	judged := func(recordPath string) printedResult {
		test.Helper()
		exit, printed, said := runGuard("-state", state, "-tonight", recordPath)
		if exit != 0 {
			test.Fatalf("judging %s exited %d:\n%s%s", filepath.Base(recordPath), exit, printed, said)
		}
		var result printedResult
		if decodeError := json.Unmarshal([]byte(printed), &result); decodeError != nil {
			test.Fatalf("printed %q: %v", printed, decodeError)
		}
		return result
	}

	loaded := judged(writeCheckingRecord(test, records, "checking-loaded.json", 20, false))
	if loaded.Verdict != bench_results.GuardVerdictUnmeasured ||
		!strings.Contains(loaded.Reason, "is never compared with them") {
		test.Errorf("a loaded checking night against a fixing baseline printed %+v", loaded)
	}
	if baseline, readRecordError := readRecord(baselinePath); readRecordError != nil || baseline.NoFix {
		test.Fatalf("a loaded checking night moved the fixing baseline (%v)", readRecordError)
	}

	quiet := judged(writeCheckingRecord(test, records, "checking-quiet.json", 20, true))
	if quiet.Verdict != bench_results.GuardVerdictPass || quiet.Reason != checkingBaselineReason ||
		quiet.BaselineMedianSeconds != 20 {
		test.Errorf("the first quiet checking night printed %+v", quiet)
	}
	if baseline, readRecordError := readRecord(baselinePath); readRecordError != nil || !baseline.NoFix {
		test.Fatalf("the first quiet checking night is not the baseline (%v)", readRecordError)
	}
	if moves := lines(test, filepath.Join(state, "baselines-ahra.jsonl")); len(moves) != 2 ||
		!strings.Contains(moves[1], "the instrument changed") {
		test.Errorf("the baseline moves on record are %q", moves)
	}

	exit, _, said := runGuard("-state", state, "-tonight", writeRecord(test, records, "fixing-again.json", 26, true))
	if exit != 1 || !strings.Contains(said, "a fixing run is never compared with a checking one") {
		test.Errorf("a fixing night against the checking baseline exited %d and said %q, want refused", exit, said)
	}
}
