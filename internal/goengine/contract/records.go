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
	// KindSummary closes every checking stream.
	KindSummary Kind = "summary"
)

// Record is one line of the stream. The set is closed, so only this package's record types are records,
// and a stream can carry nothing the front door has no case for.
type Record interface {
	kind() Kind
}

// ProvenanceRecord says which engine wrote the stream and from what source.
//
// The contract's provenance also lists `swiftSyntax` and `swiftFormat`, the versions of two Swift
// libraries. They are left out here: a Go engine links neither, and an empty version would read as one
// it could not find rather than one it does not have. The front door ignores a field it is not sent.
type ProvenanceRecord struct {
	Contract int    `json:"contract"`
	Engine   string `json:"engine"`
	Version  string `json:"version"`
	// Commit is 40 hex digits, or `dev`.
	Commit string `json:"commit"`
	// SourceTreeModified drives the warning that no commit reproduces these findings.
	SourceTreeModified bool `json:"sourceTreeModified"`
	// Toolchain is the Go toolchain the engine was built with, as runtime.Version spells it.
	Toolchain string `json:"toolchain"`
}

// ProjectRecord is what the run looked at: every file the project holds, the ones that are ours, the
// ones in scope, and each file left out with its reason.
type ProjectRecord struct {
	Root                string `json:"root"`
	Package             string `json:"package"`
	ElapsedMilliseconds int    `json:"elapsedMilliseconds"`
	FilesInPackage      int    `json:"filesInPackage"`
	FilesOurs           int    `json:"filesOurs"`
	FilesInScope        int    `json:"filesInScope"`
	// ScopeDescription is printed in the parenthesis of `N in scope (…)`; empty on a whole-project run.
	ScopeDescription string          `json:"scopeDescription"`
	Targets          []ProjectTarget `json:"targets"`
	Excluded         []ExcludedFile  `json:"excluded"`
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

// UnreadableRecord is a file the engine could not read, so nothing in it was checked. It goes between the
// project record and the first phase record, and it makes the run incomplete.
type UnreadableRecord struct {
	File string `json:"file"`
	// Error is a sentence.
	Error string `json:"error"`
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

// FindingRecord is one finding. Lines and columns are 1-based, and columns count UTF-8 bytes.
type FindingRecord struct {
	Source Source `json:"source"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	// EndLine and EndColumn are exclusive and optional. Zero is never a 1-based position, so zero means
	// not known and is left out.
	EndLine   int      `json:"endLine,omitempty"`
	EndColumn int      `json:"endColumn,omitempty"`
	Severity  Severity `json:"severity"`
	Rule      string   `json:"rule"`
	MessageId string   `json:"messageId"`
	Message   string   `json:"message"`
	// Fixes are what a `--no-fix` run withheld, never instructions for the front door.
	Fixes []Edit `json:"fixes"`
	// Suggestions are repairs offered and never applied.
	Suggestions []Suggestion `json:"suggestions"`
}

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

// FixRecord is the fix and format phase's summary, which the front door prints as an edit.Summary.
type FixRecord struct {
	FilesConsidered     int            `json:"filesConsidered"`
	FilesRewritten      int            `json:"filesRewritten"`
	FixesApplied        int            `json:"fixesApplied"`
	FixesRefused        int            `json:"fixesRefused"`
	RefusalsByReason    map[string]int `json:"refusalsByReason"`
	FilesReformatted    int            `json:"filesReformatted"`
	FilesNotFormatted   int            `json:"filesNotFormatted"`
	NotFormattedReasons map[string]int `json:"notFormattedReasons"`
	FormatScope         string         `json:"formatScope"`
	// ChangedFiles is every file the phase wrote, once each; its length is FilesRewritten. It is always
	// written, empty when nothing was, because the front door reads an absent or null list as "not
	// listed" rather than "no files changed" (see MarshalJSON).
	ChangedFiles []ChangedFile `json:"changedFiles"`
}

// ChangedFile is one file the fix phase wrote: its fixes counted by the rule that made them, and whether
// the formatter changed it.
type ChangedFile struct {
	File      string         `json:"file"`
	FixedBy   map[string]int `json:"fixedBy"`
	Formatted bool           `json:"formatted"`
}

// TypesRecord summarizes the types phase.
type TypesRecord struct {
	Diagnostics         int `json:"diagnostics"`
	Files               int `json:"files"`
	ElapsedMilliseconds int `json:"elapsedMilliseconds"`
	// FilesWithoutRecord are files the engine cannot vouch for, each a gap that makes the run incomplete.
	FilesWithoutRecord []string `json:"filesWithoutRecord"`
	// Build says in words how the type information was had.
	Build string `json:"build"`
}

// LintRecord summarizes the lint phase, for the lint line and its coverage notes.
type LintRecord struct {
	Findings            int `json:"findings"`
	RulesRun            int `json:"rulesRun"`
	FilesWalked         int `json:"filesWalked"`
	NodesVisited        int `json:"nodesVisited"`
	ElapsedMilliseconds int `json:"elapsedMilliseconds"`
	// ReusedFrom names the phase whose walk was reused, and is empty when lint walked on its own.
	ReusedFrom           string         `json:"reusedFrom"`
	RulesSilent          []string       `json:"rulesSilent"`
	RulesWatchedAndQuiet int            `json:"rulesWatchedAndQuiet"`
	Crashes              []Crash        `json:"crashes"`
	RulesScopedOff       map[string]int `json:"rulesScopedOff"`
	RulesNotConfigured   []string       `json:"rulesNotConfigured"`
	// ConfigNote is the line saying which defaults ran when no config was read, empty when one was.
	ConfigNote string `json:"configNote"`

	// WithheldBySuppression and WithheldAsGenerated are additive within contract 3, which the front door
	// ignores until it reads them, as it ignores any field the contract does not list. Each counts by rule the
	// findings a run found and did not write: those a //cohere:ignore comment withheld, and those of a house
	// rule in a generated file. Without them a run that hid everything reads like a run that found nothing.
	// Both are always written as objects, `{}` when empty (see MarshalJSON).
	WithheldBySuppression map[string]int `json:"withheldBySuppression"`
	WithheldAsGenerated   map[string]int `json:"withheldAsGenerated"`
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

// PhaseRecord is one phase's outcome. Every run writes exactly one per phase, in pipeline order,
// including the phases that did not run, because a phase absent from the report reads the same as one
// the reporter forgot.
type PhaseRecord struct {
	Name                PhaseName `json:"name"`
	Outcome             Outcome   `json:"outcome"`
	ElapsedMilliseconds int       `json:"elapsedMilliseconds"`
	Detail              string    `json:"detail"`
}

// RuleRecord is one rule, under `--rules` and `--rules-enabled`.
type RuleRecord struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
}

// SummaryRecord closes a checking stream. The front door compares each of its fields against what the
// records before it showed.
type SummaryRecord struct {
	// Findings is the number of finding records written.
	Findings int `json:"findings"`
	// Complete is false when any file or requested phase that could have produced a finding did not run.
	Complete bool `json:"complete"`
	// ExitCode is what the engine will exit with.
	ExitCode int `json:"exitCode"`
}

func (ProvenanceRecord) kind() Kind { return KindProvenance }
func (ProjectRecord) kind() Kind    { return KindProject }
func (UnreadableRecord) kind() Kind { return KindUnreadable }
func (FindingRecord) kind() Kind    { return KindFinding }
func (FixRecord) kind() Kind        { return KindFix }
func (TypesRecord) kind() Kind      { return KindTypes }
func (LintRecord) kind() Kind       { return KindLint }
func (PhaseRecord) kind() Kind      { return KindPhase }
func (RuleRecord) kind() Kind       { return KindRule }
func (SummaryRecord) kind() Kind    { return KindSummary }

// MarshalJSON writes the withheld counts as `{}` when there are none, never `null`, so a reader of the field
// never has to tell "none withheld" from "not counted".
func (record LintRecord) MarshalJSON() ([]byte, error) {
	// withoutMarshaler has LintRecord's fields and not its methods, so encoding it does not recurse here.
	type withoutMarshaler LintRecord
	counted := withoutMarshaler(record)
	if counted.WithheldBySuppression == nil {
		counted.WithheldBySuppression = map[string]int{}
	}
	if counted.WithheldAsGenerated == nil {
		counted.WithheldAsGenerated = map[string]int{}
	}
	return encodeWithoutHTMLEscaping(counted)
}

// MarshalJSON writes an empty ChangedFiles as `[]`, never `null`. The front door decodes the list into a
// pointer and reads null as "not listed", which would make a run that wrote nothing look like a run that
// did not say.
func (record FixRecord) MarshalJSON() ([]byte, error) {
	// withoutMarshaler has FixRecord's fields and not its methods, so encoding it does not recurse here.
	type withoutMarshaler FixRecord
	listed := withoutMarshaler(record)
	if listed.ChangedFiles == nil {
		listed.ChangedFiles = []ChangedFile{}
	}
	return encodeWithoutHTMLEscaping(listed)
}
