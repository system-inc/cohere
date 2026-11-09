// Package contract is the record stream the Go engine prints on stdout, swift/Contract.md's contract 3,
// which the front door already renders for cohere-swift.
//
// The JSON names here are the contract's and the front door's (command/cohere/swift_records.go), spelled
// verbatim. Speaking the contract the front door already renders is what gives a Go run the same finding
// lines, phase line, coverage notes, footer and exit code as a TypeScript or Swift run, with no printer
// written twice.
package contract

// Version is the version of swift/Contract.md this engine speaks, written in the provenance record and
// checked against the front door's `--contract`.
const Version = 3

// Kind is a record's `kind`. Its values are the contract's own spellings, camelCase, because the front
// door dispatches on them byte for byte.
type Kind string

const (
	// KindProvenance opens every stream.
	KindProvenance Kind = "provenance"
	// KindProject describes what the run looked at, the counterpart of `graph built in`.
	KindProject Kind = "project"
	// KindUnreadable names one file the engine could not read.
	KindUnreadable Kind = "unreadable"
	// KindFinding is one finding.
	KindFinding Kind = "finding"
	// KindFix summarizes the fix and format phase.
	KindFix Kind = "fix"
	// KindTypes summarizes the types phase.
	KindTypes Kind = "types"
	// KindLint summarizes the lint phase.
	KindLint Kind = "lint"
	// KindPhase is one phase's outcome.
	KindPhase Kind = "phase"
	// KindRule is one rule, under `--rules` and `--rules-enabled`.
	KindRule Kind = "rule"
	// KindInputs names what a check run read, just before its summary, for the front door's replay.
	KindInputs Kind = "inputs"
	// KindTiming says what a check run's CPU went to, under `--timing`, after the lint record.
	KindTiming Kind = "timing"
	// KindDuplicates is the duplicates report under `--duplicates`, after the unused phase's record.
	KindDuplicates Kind = "duplicates"
	// KindSummary closes every checking stream.
	KindSummary Kind = "summary"
)

// Record is one line of the stream. The set is closed, so only this package's record types are records,
// and a stream can carry nothing the front door has no case for.
type Record interface {
	kind() Kind
}

// ProjectTarget is one unit the project builds, for Swift a target and for Go a module.
type ProjectTarget struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Files        int    `json:"files"`
	LanguageMode string `json:"languageMode"`
}

// ExcludedFile is a file the run left out on purpose, printed as a coverage note grouped by its reason.
type ExcludedFile struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// Source is which part of the engine produced a finding.
type Source string

const (
	// SourceCompiler is a load, parse or type error.
	SourceCompiler Source = "compiler"
	// SourceRule is a rule's finding.
	SourceRule Source = "rule"
	// SourceFormat is a file the formatter would change, under `--no-fix`.
	SourceFormat Source = "format"
)

// Severity is what a finding was called. Both count toward the exit code; it is kept so the line can say
// which one the compiler called it.
type Severity string

const (
	// SeverityError is an error.
	SeverityError Severity = "error"
	// SeverityWarning is a warning.
	SeverityWarning Severity = "warning"
)

// Edit replaces the bytes from Start to End, 0-based UTF-8 offsets into the file as the engine read it.
type Edit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// Suggestion is a repair offered and never applied.
type Suggestion struct {
	Message string `json:"message"`
	Fixes   []Edit `json:"fixes"`
}

// ChangedFile is one file the fix phase wrote: its fixes counted by the rule that made them, and whether
// the formatter changed it.
type ChangedFile struct {
	File      string         `json:"file"`
	FixedBy   map[string]int `json:"fixedBy"`
	Formatted bool           `json:"formatted"`
}

// Crash is a rule that failed on a file, which makes the run incomplete.
type Crash struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// PhaseName is one of the four phases, in pipeline order fix, types, lint, unused.
type PhaseName string

const (
	// PhaseFix is the fix and format phase.
	PhaseFix PhaseName = "fix"
	// PhaseTypes is the load and type check.
	PhaseTypes PhaseName = "types"
	// PhaseLint is the rules.
	PhaseLint PhaseName = "lint"
	// PhaseUnused is the `--unused` report.
	PhaseUnused PhaseName = "unused"
)

// Outcome is how a phase ended, phases.go's outcomes spelled in camelCase as the contract spells them.
type Outcome string

const (
	// OutcomeRan is a phase that ran.
	OutcomeRan Outcome = "ran"
	// OutcomeSkipped is a phase left out; with the detail `not requested` it is not a gap.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeNotReached is a phase an earlier one cut off.
	OutcomeNotReached Outcome = "notReached"
	// OutcomeReused is a phase whose result came from an earlier run.
	OutcomeReused Outcome = "reused"
)
