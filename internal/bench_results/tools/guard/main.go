// Command guard is the comparison half of the nightly speed guard: it holds tonight's bench/quiet_machine.sh
// record against the baseline of record for its project (bench_results.Guard), and keeps the verdicts and
// every move of the baseline on file in a state directory of its own:
//
//	guard -state <directory> -tonight <record.json> [-threshold 0.10]
//	guard -state <directory> -accept <record.json> -because <text>
//	guard -state <directory> -project <name> -lost <reason>
//
// The first form reads <directory>/baseline-<project>.json, appends tonight's verdict to
// <directory>/verdicts-<project>.jsonl, and prints the result as JSON with how many nights in a row, tonight's
// included, went unmeasured. With no baseline yet, a quiet tonight becomes the baseline and passes, and so does a
// quiet tonight whose cold runs check without fixing against a baseline whose runs fixed: the guard moved to
// checking runs (#8sqsgee), and the one change of instrument it was told of moves its baseline with it, on record,
// where every other difference between the two is refused. It exits 0 for Pass, Faster and Unmeasured, 3 for
// Regressed, and 1 when nothing could be judged.
//
// The second form makes a quiet record the baseline and appends why to <directory>/baselines-<project>.jsonl,
// so the baseline only ever moves with a reason on record, never by itself to last night's number, which
// would let a slow creep pass one small step at a time. It exits 1 for a record with no quiet cold number,
// or with no reason given.
//
// The third form records a night that left no record at all, because the bench refused its copy, crashed or ran
// out of time: an Unmeasured verdict with the reason, printed as the first form prints one, exit 0. A night
// that fails before the bench can record is still a night without a number, and leaving it off the verdicts
// would let a guard that never runs read as one whose last night passed.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/bench_results"
)

// regressedExit is the exit of a regressed night, apart from 1 so a nightly script can tell a slower engine
// from a guard that could not judge.
const regressedExit = 3

// firstBaselineReason is the reason on record when a project's first quiet night becomes its baseline.
const firstBaselineReason = "the first quiet night, now the baseline"

// checkingBaselineReason is the reason on record when a quiet night of checking cold runs replaces a baseline whose
// cold runs fixed, the change of instrument the guard made on 2026-11-12 (#8sqsgee): a fixing run's work moves with
// what the tree holds, so the two are never compared, and the first quiet checking night starts the new baseline.
const checkingBaselineReason = "the instrument changed: the cold runs now check without fixing and the baseline " +
	"fixed, so this first quiet checking night is the new baseline"

// verdictLine is one night in verdicts-<project>.jsonl.
type verdictLine struct {
	RecordedAt           string                     `json:"recordedAt"`
	Verdict              bench_results.GuardVerdict `json:"verdict"`
	Ratio                float64                    `json:"ratio"`
	TonightMedianSeconds float64                    `json:"tonightMedianSeconds"`
	CohereCommit         string                     `json:"cohereCommit"`
	ProjectCommit        string                     `json:"projectCommit"`
	// Reason says why a night went unmeasured, as the printed result does.
	Reason string `json:"reason,omitempty"`
}

// baselineLine is one move of the baseline in baselines-<project>.jsonl.
type baselineLine struct {
	AcceptedAt string           `json:"acceptedAt"`
	Accepted   acceptedBaseline `json:"accepted"`
	Because    string           `json:"because"`
}

// acceptedBaseline names the record that became the baseline, enough to find it in bench/results.
type acceptedBaseline struct {
	RecordedAt         string  `json:"recordedAt"`
	CohereCommit       string  `json:"cohereCommit"`
	ProjectCommit      string  `json:"projectCommit"`
	QuietMedianSeconds float64 `json:"quietMedianSeconds"`
}

// printedResult is what a judged night prints: the result, and the unmeasured nights in a row it ends on.
type printedResult struct {
	bench_results.GuardResult
	UnmeasuredStreak int `json:"unmeasuredStreak"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the command, with its arguments and output given, so a test runs it in process and reads its exit.
func run(arguments []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("guard", flag.ContinueOnError)
	flags.SetOutput(stderr)
	state := flags.String("state", "", "the directory holding the baselines and the verdicts")
	tonight := flags.String("tonight", "", "tonight's record, to judge against the baseline")
	threshold := flags.Float64("threshold", 0.10, "the share of the baseline a quiet median may move either way")
	accept := flags.String("accept", "", "a record to make the baseline")
	because := flags.String("because", "", "why the accepted record is the baseline now")
	project := flags.String("project", "", "the project whose night was lost")
	lost := flags.String("lost", "", "why the night left no record")
	if parseError := flags.Parse(arguments); parseError != nil {
		return 1
	}
	var exit int
	var runError error
	switch {
	case *state == "" || countGiven(*tonight, *accept, *lost) != 1 || (*project != "") != (*lost != "") ||
		flags.NArg() != 0:
		runError = errors.New(
			"usage: guard -state <directory> -tonight <record.json> [-threshold 0.10], " +
				"or guard -state <directory> -accept <record.json> -because <text>, " +
				"or guard -state <directory> -project <name> -lost <reason>",
		)
	case *accept != "":
		runError = acceptBaseline(*state, *accept, *because)
	case *lost != "":
		exit, runError = recordLostNight(*state, *project, *lost, stdout)
	default:
		exit, runError = judge(*state, *tonight, *threshold, stdout)
	}
	if runError != nil {
		// Ignored: the exit code below says the run failed even when standard error cannot.
		fmt.Fprintf(stderr, "guard: %v\n", runError)
		return 1
	}
	return exit
}

// judge holds tonight's record against the baseline, records the verdict, prints it, and gives the exit.
func judge(state string, tonightPath string, threshold float64, stdout io.Writer) (int, error) {
	tonight, readRecordError := readRecord(tonightPath)
	if readRecordError != nil {
		return 1, readRecordError
	}
	// A modified engine's record fails Validate for that alone, and Guard calls its night Unmeasured, which is
	// the verdict a nightly script needs rather than a refusal; every other record is held to its own runs.
	if !tonight.Cohere.Dirty {
		if validateError := bench_results.Validate(tonight); validateError != nil {
			return 1, fmt.Errorf("%s: %w", tonightPath, validateError)
		}
	}
	if projectNameError := plainProjectName(tonight.Project.Name); projectNameError != nil {
		return 1, projectNameError
	}
	if createDirectoryError := os.MkdirAll(state, 0o755); createDirectoryError != nil {
		return 1, createDirectoryError
	}
	baselinePath := filepath.Join(state, "baseline-"+tonight.Project.Name+".json")
	var result bench_results.GuardResult
	baseline, readBaselineError := readRecord(baselinePath)
	switch {
	case errors.Is(readBaselineError, os.ErrNotExist):
		var newBaselineNightError error
		if result, newBaselineNightError = newBaselineNight(state, tonight, "there is no baseline yet",
			firstBaselineReason); newBaselineNightError != nil {
			return 1, newBaselineNightError
		}
	case readBaselineError != nil:
		return 1, readBaselineError
	default:
		if validateError := bench_results.Validate(baseline); validateError != nil {
			return 1, fmt.Errorf("%s: %w", baselinePath, validateError)
		}
		if !baseline.NoFix && tonight.NoFix {
			retired := fmt.Sprintf(
				"the cold runs now check without fixing, and the baseline of %s fixed, "+
					"so it is never compared with them",
				baseline.RecordedAt,
			)
			var newBaselineNightError error
			if result, newBaselineNightError = newBaselineNight(state, tonight, retired,
				checkingBaselineReason); newBaselineNightError != nil {
				return 1, newBaselineNightError
			}
			break
		}
		var guardError error
		if result, guardError = bench_results.Guard(baseline, tonight, threshold); guardError != nil {
			return 1, guardError
		}
	}
	return recordNight(state, result, verdictLine{
		RecordedAt:           tonight.RecordedAt,
		Verdict:              result.Verdict,
		Ratio:                result.Ratio,
		TonightMedianSeconds: result.TonightMedianSeconds,
		CohereCommit:         tonight.Cohere.Commit,
		ProjectCommit:        tonight.Project.Commit,
		Reason:               result.Reason,
	}, stdout)
}

// recordLostNight records a night that left no record as Unmeasured, with the baseline's number and commits
// beside it when there is a baseline, so the printed result reads like any other unmeasured night's.
func recordLostNight(state string, project string, reason string, stdout io.Writer) (int, error) {
	if projectNameError := plainProjectName(project); projectNameError != nil {
		return 1, projectNameError
	}
	if createDirectoryError := os.MkdirAll(state, 0o755); createDirectoryError != nil {
		return 1, createDirectoryError
	}
	result := bench_results.GuardResult{
		Verdict: bench_results.GuardVerdictUnmeasured,
		Project: project,
		Reason:  reason,
	}
	baselinePath := filepath.Join(state, "baseline-"+project+".json")
	baseline, readBaselineError := readRecord(baselinePath)
	switch {
	case errors.Is(readBaselineError, os.ErrNotExist):
	case readBaselineError != nil:
		return 1, readBaselineError
	default:
		if validateError := bench_results.Validate(baseline); validateError != nil {
			return 1, fmt.Errorf("%s: %w", baselinePath, validateError)
		}
		// A valid baseline file is a quiet one, since -accept and the first night write only those.
		if baselineQuiet := baseline.Modes[bench_results.ModeCold].Quiet; baselineQuiet != nil {
			result.BaselineMedianSeconds = baselineQuiet.Median
		}
		result.BaselineCohereCommit = baseline.Cohere.Commit
		result.BaselineProjectCommit = baseline.Project.Commit
	}
	return recordNight(state, result, verdictLine{
		RecordedAt: time.Now().UTC().Format(time.RFC3339),
		Verdict:    result.Verdict,
		Reason:     reason,
	}, stdout)
}

// recordNight appends a night's verdict to its project's verdicts, prints its result with the unmeasured
// streak the verdicts now end on, and gives the night's exit.
func recordNight(
	state string,
	result bench_results.GuardResult,
	night verdictLine,
	stdout io.Writer,
) (int, error) {
	verdictsPath := filepath.Join(state, "verdicts-"+result.Project+".jsonl")
	if appendError := appendLine(verdictsPath, night); appendError != nil {
		return 1, appendError
	}
	verdicts, readVerdictsError := readVerdicts(verdictsPath)
	if readVerdictsError != nil {
		return 1, readVerdictsError
	}
	printed, encodeError := json.MarshalIndent(
		printedResult{GuardResult: result, UnmeasuredStreak: bench_results.UnmeasuredStreak(verdicts)},
		"",
		"  ",
	)
	if encodeError != nil {
		return 1, encodeError
	}
	if _, printError := fmt.Fprintf(stdout, "%s\n", printed); printError != nil {
		return 1, fmt.Errorf("printing the result: %w", printError)
	}
	if result.Verdict == bench_results.GuardVerdictRegressed {
		return regressedExit, nil
	}
	return 0, nil
}

// newBaselineNight is the verdict of a night with no baseline to be compared with, none yet or one tonight's runs are
// never compared with, which without says: a quiet tonight becomes the baseline with because on record and passes
// against itself, and a tonight with no quiet number is Unmeasured, leaving the baseline as it was.
func newBaselineNight(
	state string,
	tonight bench_results.Record,
	without string,
	because string,
) (bench_results.GuardResult, error) {
	result := bench_results.GuardResult{
		Project:              tonight.Project.Name,
		TonightCohereCommit:  tonight.Cohere.Commit,
		TonightProjectCommit: tonight.Project.Commit,
	}
	if narrowedError := refuseNarrowedBaseline(tonight); narrowedError != nil {
		return result, narrowedError
	}
	tonightQuiet := tonight.Modes[bench_results.ModeCold].Quiet
	switch {
	case tonight.Cohere.Dirty:
		result.Verdict = bench_results.GuardVerdictUnmeasured
		result.Reason = fmt.Sprintf(
			"%s, and tonight's cohere %s was built from a modified tree, which no commit reproduces",
			without,
			tonight.Cohere.Commit,
		)
		return result, nil
	case tonightQuiet == nil:
		result.Verdict = bench_results.GuardVerdictUnmeasured
		result.Reason = fmt.Sprintf("%s, and no %s run tonight was quiet to start one", without, bench_results.ModeCold)
		return result, nil
	}
	if writeBaselineError := writeBaseline(state, tonight, because); writeBaselineError != nil {
		return result, writeBaselineError
	}
	result.Verdict = bench_results.GuardVerdictPass
	result.BaselineMedianSeconds = tonightQuiet.Median
	result.TonightMedianSeconds = tonightQuiet.Median
	result.Ratio = 1
	result.BaselineCohereCommit = tonight.Cohere.Commit
	result.BaselineProjectCommit = tonight.Project.Commit
	result.Reason = because
	return result, nil
}

// acceptBaseline makes the record at recordPath its project's baseline, with because on record.
func acceptBaseline(state string, recordPath string, because string) error {
	if strings.TrimSpace(because) == "" {
		return errors.New("-because is required: every move of the baseline says why")
	}
	record, readRecordError := readRecord(recordPath)
	if readRecordError != nil {
		return readRecordError
	}
	if validateError := bench_results.Validate(record); validateError != nil {
		return fmt.Errorf("%s: %w", recordPath, validateError)
	}
	if projectNameError := plainProjectName(record.Project.Name); projectNameError != nil {
		return projectNameError
	}
	if narrowedError := refuseNarrowedBaseline(record); narrowedError != nil {
		return fmt.Errorf("%s: %w", recordPath, narrowedError)
	}
	if record.Modes[bench_results.ModeCold].Quiet == nil {
		return fmt.Errorf(
			"%s has no quiet %s run, and a baseline must be a quiet number",
			recordPath,
			bench_results.ModeCold,
		)
	}
	if createDirectoryError := os.MkdirAll(state, 0o755); createDirectoryError != nil {
		return createDirectoryError
	}
	return writeBaseline(state, record, because)
}

// refuseNarrowedBaseline refuses a record narrowed to one engine's project (bench_results.Record.Engine) as a
// baseline, on both paths that make one: the first night and -accept. The nightly guard times whole roots, and
// bench_results.Guard refuses to compare a narrowed record with a whole-root one, so a narrowed baseline would
// turn every later night into a refusal that points at the baseline rather than at the record that caused it.
func refuseNarrowedBaseline(record bench_results.Record) error {
	if record.Engine == "" {
		return nil
	}
	return fmt.Errorf(
		"the record checked the %s project alone (engine %s), and a baseline is a record of the whole root",
		record.Engine,
		record.Engine,
	)
}

// writeBaseline writes record as its project's baseline and appends the move, with because, to the baselines
// log. The record is written beside the baseline and renamed over it, so a guard that dies partway leaves
// the old baseline whole rather than a file no night can be judged against.
func writeBaseline(state string, record bench_results.Record, because string) error {
	encoded, encodeError := bench_results.Encode(record)
	if encodeError != nil {
		return encodeError
	}
	baselinePath := filepath.Join(state, "baseline-"+record.Project.Name+".json")
	if writeError := os.WriteFile(baselinePath+".partial", encoded, 0o644); writeError != nil {
		return writeError
	}
	if renameError := os.Rename(baselinePath+".partial", baselinePath); renameError != nil {
		return renameError
	}
	return appendLine(filepath.Join(state, "baselines-"+record.Project.Name+".jsonl"), baselineLine{
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
		Accepted: acceptedBaseline{
			RecordedAt:         record.RecordedAt,
			CohereCommit:       record.Cohere.Commit,
			ProjectCommit:      record.Project.Commit,
			QuietMedianSeconds: record.Modes[bench_results.ModeCold].Quiet.Median,
		},
		Because: because,
	})
}

// readRecord decodes a record, refusing a field the record does not have, as bench_results.Read does. It is
// not validated here: the caller decides, since a modified engine's night is judged rather than refused.
func readRecord(path string) (bench_results.Record, error) {
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return bench_results.Record{}, readError
	}
	var record bench_results.Record
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(&record); decodeError != nil {
		return bench_results.Record{}, fmt.Errorf("%s: %w", path, decodeError)
	}
	return record, nil
}

// countGiven is how many of values are not empty, so the command can hold its forms to one at a time.
func countGiven(values ...string) int {
	given := 0
	for _, value := range values {
		if value != "" {
			given++
		}
	}
	return given
}

// plainProjectName refuses a project name that would put a state file outside the state directory.
func plainProjectName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("the project name %q cannot name a state file", name)
	}
	return nil
}

// appendLine appends line to path as JSON on a line of its own, creating the file on the first line.
func appendLine[Line verdictLine | baselineLine](path string, line Line) error {
	encoded, encodeError := json.Marshal(line)
	if encodeError != nil {
		return encodeError
	}
	file, openError := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if openError != nil {
		return openError
	}
	if _, writeError := file.Write(append(encoded, '\n')); writeError != nil {
		// Ignored: the write already failed, and that is the error that says what was lost.
		file.Close()
		return fmt.Errorf("%s: %w", path, writeError)
	}
	// The close is checked, since a line the file system never took is a verdict lost.
	return file.Close()
}

// readVerdicts reads every verdict in the verdicts log, oldest first.
func readVerdicts(path string) ([]bench_results.GuardVerdict, error) {
	file, openError := os.Open(path)
	if openError != nil {
		return nil, openError
	}
	defer file.Close() // Ignored: the file was only read, so a failed close loses nothing.
	verdicts := []bench_results.GuardVerdict{}
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		var night verdictLine
		if decodeError := json.Unmarshal(scanner.Bytes(), &night); decodeError != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, decodeError)
		}
		verdicts = append(verdicts, night.Verdict)
	}
	return verdicts, scanner.Err()
}
