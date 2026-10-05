// Package benchresults is the recorded form of a bench/quiet_machine.sh run: one JSON file per run in
// bench/results, committed, which the website's benchmarks page reads (#rwgs6j2, for #7tmkt32).
//
// A quiet number is the only number the benchmark reports, so the record is shaped so that one cannot be
// claimed without being earned. A run's Quiet is its loads against the ceiling, nothing else. A mode
// carries a Quiet summary only when at least one of its runs was quiet, and the summary is what those
// runs give. Validate holds all three, and both the writer and every reader call it, so a record that
// disagrees with its own runs is refused wherever it is opened.
package benchresults

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SchemaVersion is the version of the record's shape. A reader refuses a record of any other.
const SchemaVersion = 1

// Directory is where records are committed, relative to the module root.
const Directory = "bench/results"

// Mode is what a measured run does: Cold runs with --no-cache, Warm replays an unchanged tree, and Edit runs
// after a one-line comment is added to one function body.
type Mode string

const (
	ModeCold Mode = "Cold"
	ModeWarm Mode = "Warm"
	ModeEdit Mode = "Edit"
)

// Modes are the measured modes, in the order the report prints them.
var Modes = []Mode{ModeCold, ModeWarm, ModeEdit}

// Record is one benchmark run, recorded.
type Record struct {
	Schema     int     `json:"schema"`
	RecordedAt string  `json:"recordedAt"`
	Cohere     Cohere  `json:"cohere"`
	Project    Project `json:"project"`
	Machine    Machine `json:"machine"`

	// LoadCeiling is the highest one-minute load a quiet run may start or end at.
	LoadCeiling float64 `json:"loadCeiling"`
	// SettleSeconds is how long each measured run waited for the load to reach the ceiling.
	SettleSeconds float64 `json:"settleSeconds"`
	// Rounds is how many runs of each mode were measured.
	Rounds int `json:"rounds"`
	// Edit is the file the Edit runs changed, relative to the project.
	Edit string `json:"edit"`

	Runs  []Run                `json:"runs"`
	Modes map[Mode]ModeSummary `json:"modes"`
}

// Cohere is the engine every run used.
type Cohere struct {
	// Commit is the full commit the engine was built from.
	Commit string `json:"commit"`
	// Version is the first line of the engine's --version.
	Version string `json:"version"`
	// Dirty is true for an engine built from a tree with uncommitted changes, which no commit reproduces.
	Dirty bool `json:"dirty"`
}

// Project is the project measured, as the copy pinned it.
type Project struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

// Machine is the hardware the runs were measured on. It names no host and no person.
type Machine struct {
	Model       string `json:"model"`
	Processor   string `json:"processor"`
	Cores       int    `json:"cores"`
	MemoryBytes int64  `json:"memoryBytes"`
	OS          string `json:"os"`
}

// Run is one measured run.
type Run struct {
	Mode  Mode `json:"mode"`
	Round int  `json:"round"`
	// VerdictSeconds is what a developer waits for: until the engine has its exit code.
	VerdictSeconds float64 `json:"verdictSeconds"`
	// SettledSeconds is until every process the run started has exited, its cache write included.
	SettledSeconds float64 `json:"settledSeconds"`
	// EngineSeconds is the engine's own wall clock, from its --json summary.
	EngineSeconds float64 `json:"engineSeconds"`
	LoadBefore    float64 `json:"loadBefore"`
	LoadAfter     float64 `json:"loadAfter"`
	// Quiet is true when the load was at or under the ceiling both before and after the run.
	Quiet bool `json:"quiet"`
	// Findings and Cache are what the run reported about itself (RunOutput).
	Findings int    `json:"findings"`
	Cache    string `json:"cache"`
	Exit     int    `json:"exit"`
}

// ModeSummary is one mode's verdict seconds.
type ModeSummary struct {
	// Quiet is the quiet runs' best, median and worst, and is absent when no run of the mode was quiet.
	Quiet *QuietSummary `json:"quiet,omitempty"`
	// LoadedVerdictSeconds are the loaded runs' verdict seconds, sorted: shown, never reported as the number.
	LoadedVerdictSeconds []float64 `json:"loadedVerdictSeconds"`
}

// QuietSummary is a mode's number: taken from quiet runs only.
type QuietSummary struct {
	Count  int     `json:"count"`
	Best   float64 `json:"best"`
	Median float64 `json:"median"`
	Worst  float64 `json:"worst"`
}

// Summarize is the mode summaries runs give, the same way quiet_machine.sh prints them: the median of an
// even count is the lower middle.
func Summarize(runs []Run) map[Mode]ModeSummary {
	summaries := map[Mode]ModeSummary{}
	for _, mode := range Modes {
		quiet, loaded := []float64{}, []float64{}
		for _, run := range runs {
			if run.Mode != mode {
				continue
			}
			if run.Quiet {
				quiet = append(quiet, run.VerdictSeconds)
			} else {
				loaded = append(loaded, run.VerdictSeconds)
			}
		}
		sort.Float64s(quiet)
		sort.Float64s(loaded)
		summary := ModeSummary{LoadedVerdictSeconds: loaded}
		if len(quiet) > 0 {
			summary.Quiet = &QuietSummary{Count: len(quiet), Best: quiet[0], Median: quiet[(len(quiet)-1)/2], Worst: quiet[len(quiet)-1]}
		}
		summaries[mode] = summary
	}
	return summaries
}

// Validate refuses a record that is not the honest account of its own runs: a run called quiet whose loads
// were over the ceiling, or the reverse; a quiet summary that is not what the quiet runs give, including
// one on a mode with no quiet run; a mode with a run count other than the rounds; a run whose cache use
// its mode cannot have, which means the run was not what its mode says, or the instrument read nothing.
// A dirty engine is refused too, since no commit reproduces its numbers.
func Validate(record Record) error {
	if record.Schema != SchemaVersion {
		return fmt.Errorf("schema %d, want %d", record.Schema, SchemaVersion)
	}
	if record.Cohere.Dirty {
		return fmt.Errorf("cohere %s was built from a modified tree, which no commit reproduces", record.Cohere.Commit)
	}
	if len(record.Cohere.Commit) != 40 || len(record.Project.Commit) != 40 {
		return fmt.Errorf("cohere commit %q and project commit %q must each be a full commit", record.Cohere.Commit, record.Project.Commit)
	}
	if record.Rounds < 1 || record.LoadCeiling <= 0 {
		return fmt.Errorf("%d rounds at a load ceiling of %g measure nothing", record.Rounds, record.LoadCeiling)
	}
	counts := map[Mode]int{}
	for _, run := range record.Runs {
		counts[run.Mode]++
		quiet := run.LoadBefore <= record.LoadCeiling && run.LoadAfter <= record.LoadCeiling
		if run.Quiet != quiet {
			return fmt.Errorf("%s round %d is marked quiet %v at loads %g and %g against a ceiling of %g", run.Mode, run.Round,
				run.Quiet, run.LoadBefore, run.LoadAfter, record.LoadCeiling)
		}
		if run.VerdictSeconds <= 0 || run.SettledSeconds < run.VerdictSeconds {
			return fmt.Errorf("%s round %d settled at %gs before or without its verdict at %gs", run.Mode, run.Round,
				run.SettledSeconds, run.VerdictSeconds)
		}
		if run.EngineSeconds <= 0 {
			return fmt.Errorf("%s round %d has no engine seconds, so its output was not read", run.Mode, run.Round)
		}
		if err := cacheFitsMode(run); err != nil {
			return fmt.Errorf("%s round %d: %w", run.Mode, run.Round, err)
		}
	}
	for _, mode := range Modes {
		if counts[mode] != record.Rounds {
			return fmt.Errorf("%d %s runs for %d rounds", counts[mode], mode, record.Rounds)
		}
	}
	if len(counts) != len(Modes) {
		return fmt.Errorf("runs of a mode other than %v", Modes)
	}
	want, err := json.Marshal(Summarize(record.Runs))
	if err != nil {
		return err
	}
	got, err := json.Marshal(record.Modes)
	if err != nil {
		return err
	}
	if string(got) != string(want) {
		return fmt.Errorf("the mode summaries are not what the runs give:\n  recorded %s\n  the runs %s", got, want)
	}
	return nil
}

// cacheFitsMode refuses a cache use the run's mode cannot have: Cold runs with --no-cache, so its cache is
// off; Warm replays an unchanged tree, so it is a whole replay or every file replayed; Edit changed one
// file, so some files and not all of them were replayed.
func cacheFitsMode(run Run) error {
	replayed, inScope := 0, 0
	partial := false
	if _, err := fmt.Sscanf(run.Cache, "files %d/%d", &replayed, &inScope); err == nil {
		partial = replayed > 0 && replayed < inScope
	}
	switch {
	case run.Mode == ModeCold && run.Cache == "off":
	case run.Mode == ModeWarm && (run.Cache == "replay" || (replayed > 0 && replayed == inScope)):
	case run.Mode == ModeEdit && partial:
	default:
		return fmt.Errorf("its cache read %q, which a %s run cannot have", run.Cache, run.Mode)
	}
	return nil
}

// Encode is a record as it is committed: validated, indented, with a final newline.
func Encode(record Record) ([]byte, error) {
	if err := Validate(record); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// FileName is the record's file name: its date, the engine's commit and the machine's model.
func FileName(record Record) string {
	model := strings.Map(func(character rune) rune {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			return character
		case character >= 'A' && character <= 'Z':
			return character + 'a' - 'A'
		}
		return '-'
	}, record.Machine.Model)
	return fmt.Sprintf("%s-%s-%s.json", record.RecordedAt[:10], record.Cohere.Commit[:12], model)
}

// Read reads every record in directory, sorted by file name, and refuses the first that does not
// validate. A directory that does not exist holds no records.
func Read(directory string) ([]Record, error) {
	paths, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	records := []Record{}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var record Record
		decoder := json.NewDecoder(strings.NewReader(string(contents)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := Validate(record); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if name := FileName(record); filepath.Base(path) != name {
			return nil, fmt.Errorf("%s: a record of this run is named %s", path, name)
		}
		records = append(records, record)
	}
	return records, nil
}
