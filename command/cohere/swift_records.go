package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/release/packaging"
)

// swiftContractVersion is the version of swift/Contract.md this front door speaks.
const swiftContractVersion = 2

// swiftMode is which question a Swift run answers, and so which records it may carry.
//
// The listing modes end without a summary by design: `--version` prints one provenance record and
// `--rules` prints rule records, as the contract says. A run in the checking mode that ends without a
// summary is a crash. The mode is what lets one absence mean two different things without the front
// door guessing which.
type swiftMode string

const (
	swiftModeCheck        swiftMode = "Check"
	swiftModeVersion      swiftMode = "Version"
	swiftModeRules        swiftMode = "Rules"
	swiftModeRulesEnabled swiftMode = "RulesEnabled"
)

// The records, as swift/Contract.md defines them. Fields the contract does not list are ignored by
// leaving them out of these types, which is what the contract asks of the front door.

type swiftProvenanceRecord struct {
	Contract           *int   `json:"contract"`
	Engine             string `json:"engine"`
	Version            string `json:"version"`
	Commit             string `json:"commit"`
	SourceTreeModified bool   `json:"sourceTreeModified"`
	Toolchain          string `json:"toolchain"`
	SwiftSyntax        string `json:"swiftSyntax"`
	SwiftFormat        string `json:"swiftFormat"`
}

type swiftProjectRecord struct {
	Root                string `json:"root"`
	Package             string `json:"package"`
	ElapsedMilliseconds int    `json:"elapsedMilliseconds"`
	FilesInPackage      int    `json:"filesInPackage"`
	FilesOurs           int    `json:"filesOurs"`
	FilesInScope        int    `json:"filesInScope"`
	ScopeDescription    string `json:"scopeDescription"`
	Excluded            []struct {
		File   string `json:"file"`
		Reason string `json:"reason"`
	} `json:"excluded"`
}

// swiftUnreadableRecord is a file the engine could not read, so nothing in it was checked. Contract 2
// made it a record: before, the engine said only on stderr that it had fallen short, and the front door
// could believe it but not say which files.
type swiftUnreadableRecord struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

type swiftFindingRecord struct {
	Source    string `json:"source"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Severity  string `json:"severity"`
	Rule      string `json:"rule"`
	MessageId string `json:"messageId"`
	Message   string `json:"message"`
}

type swiftFixRecord struct {
	FilesConsidered     int            `json:"filesConsidered"`
	FilesRewritten      int            `json:"filesRewritten"`
	FixesApplied        int            `json:"fixesApplied"`
	FixesRefused        int            `json:"fixesRefused"`
	RefusalsByReason    map[string]int `json:"refusalsByReason"`
	FilesReformatted    int            `json:"filesReformatted"`
	FilesNotFormatted   int            `json:"filesNotFormatted"`
	NotFormattedReasons map[string]int `json:"notFormattedReasons"`
	FormatScope         string         `json:"formatScope"`
}

type swiftTypesRecord struct {
	Diagnostics         int      `json:"diagnostics"`
	Files               int      `json:"files"`
	ElapsedMilliseconds int      `json:"elapsedMilliseconds"`
	FilesWithoutRecord  []string `json:"filesWithoutRecord"`
}

type swiftLintRecord struct {
	Findings             int            `json:"findings"`
	RulesRun             int            `json:"rulesRun"`
	FilesWalked          int            `json:"filesWalked"`
	NodesVisited         int            `json:"nodesVisited"`
	ElapsedMilliseconds  int            `json:"elapsedMilliseconds"`
	ReusedFrom           string         `json:"reusedFrom"`
	RulesSilent          []string       `json:"rulesSilent"`
	RulesWatchedAndQuiet int            `json:"rulesWatchedAndQuiet"`
	RulesScopedOff       map[string]int `json:"rulesScopedOff"`
	RulesNotConfigured   []string       `json:"rulesNotConfigured"`
	ConfigNote           string         `json:"configNote"`
	Crashes              []struct {
		File  string `json:"file"`
		Error string `json:"error"`
	} `json:"crashes"`
}

type swiftPhaseRecord struct {
	Name                string `json:"name"`
	Outcome             string `json:"outcome"`
	ElapsedMilliseconds int    `json:"elapsedMilliseconds"`
	Detail              string `json:"detail"`
}

type swiftRuleRecord struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
}

type swiftSummaryRecord struct {
	Findings       *int   `json:"findings"`
	Complete       *bool  `json:"complete"`
	NothingToCheck string `json:"nothingToCheck"`
	ExitCode       *int   `json:"exitCode"`
}

// swiftOutcomes maps the contract's camelCase outcomes onto the ones phases.go prints.
var swiftOutcomes = map[string]phaseOutcome{
	"ran":        outcomeRan,
	"skipped":    outcomeSkipped,
	"notReached": outcomeNotReached,
	"reused":     outcomeReused,
}

// swiftRun renders one engine run's records as they arrive, through the printers a TypeScript run
// uses, and refuses any stream that breaks the contract.
//
// Every refusal is an error rather than a skipped line, for the reason the contract gives: a skipped
// record is a finding nobody sees. And every count the stream states twice is compared, because two
// numbers that should agree and do not are a defect in one of them, and printing either would be
// printing a number nobody can vouch for.
type swiftRun struct {
	out  io.Writer
	mode swiftMode

	provenance *swiftProvenanceRecord
	project    *swiftProjectRecord
	summary    *swiftSummaryRecord
	report     *pipelineReport
	rules      []swiftRuleRecord

	// findings is every finding record received; findingsInPhase only those since the last phase
	// record, which is how the types and lint records' own counts are checked.
	findings        int
	findingsInPhase int

	// nextPhase indexes phaseOrder: phases must arrive in pipeline order, each exactly once.
	nextPhase int

	// Gaps the records name outside the phase outcomes: files the compiler left no record for, and
	// files a rule crashed on. Each is already printed as a note; these count them for the summary.
	filesWithoutRecord int
	crashes            int
	unreadable         []swiftUnreadableRecord

	excludedPrinted bool
}

func newSwiftRun(out io.Writer, mode swiftMode, rootNote string, processStart time.Time) *swiftRun {
	return &swiftRun{
		out:  out,
		mode: mode,
		report: &pipelineReport{
			processStart:          processStart,
			rootNote:              rootNote,
			graphLabel:            "package",
			graphNotBuiltSentence: "no package was described and no phase ran",
		},
	}
}

// accept renders one line of the engine's stdout.
func (r *swiftRun) accept(line []byte) error {
	var envelope struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return fmt.Errorf("a line that is not a record: %q (%v)", truncateForError(line), err)
	}

	if r.summary != nil {
		return fmt.Errorf("a %q record after the summary, which is always last", envelope.Kind)
	}
	if r.provenance == nil && envelope.Kind != "provenance" {
		return fmt.Errorf("the first record was %q, and provenance is always first", envelope.Kind)
	}

	switch envelope.Kind {
	case "provenance":
		return decodeRecord(line, &swiftProvenanceRecord{}, r.acceptProvenance)
	case "project":
		return decodeRecord(line, &swiftProjectRecord{}, r.acceptProject)
	case "unreadable":
		return decodeRecord(line, &swiftUnreadableRecord{}, r.acceptUnreadable)
	case "finding":
		return decodeRecord(line, &swiftFindingRecord{}, r.acceptFinding)
	case "fix":
		return decodeRecord(line, &swiftFixRecord{}, r.acceptFix)
	case "types":
		return decodeRecord(line, &swiftTypesRecord{}, r.acceptTypes)
	case "lint":
		return decodeRecord(line, &swiftLintRecord{}, r.acceptLint)
	case "phase":
		return decodeRecord(line, &swiftPhaseRecord{}, r.acceptPhase)
	case "rule":
		return decodeRecord(line, &swiftRuleRecord{}, r.acceptRule)
	case "summary":
		return decodeRecord(line, &swiftSummaryRecord{}, r.acceptSummary)
	default:
		// An unknown kind is refused rather than ignored: it is a record this front door cannot render,
		// so whatever it carried would reach nobody.
		return fmt.Errorf("a record of unknown kind %q", envelope.Kind)
	}
}

// decodeRecord decodes a line into its record type and hands it on.
func decodeRecord[Record any](line []byte, record *Record, accept func(*Record) error) error {
	if err := json.Unmarshal(line, record); err != nil {
		return fmt.Errorf("a record that does not decode: %q (%v)", truncateForError(line), err)
	}
	return accept(record)
}

func (r *swiftRun) acceptProvenance(record *swiftProvenanceRecord) error {
	if r.provenance != nil {
		return fmt.Errorf("a second provenance record")
	}
	// Checked here as well as by the engine against `--contract`, because the flag and the record are
	// two independent ways for the versions to disagree, and the contract asks for both.
	if record.Contract == nil || *record.Contract != swiftContractVersion {
		spoken := "none"
		if record.Contract != nil {
			spoken = fmt.Sprint(*record.Contract)
		}
		return fmt.Errorf("the engine speaks contract %s and this front door speaks contract %d", spoken, swiftContractVersion)
	}
	r.provenance = record
	r.report.engineSourceTreeModified = record.SourceTreeModified
	return nil
}

func (r *swiftRun) acceptProject(record *swiftProjectRecord) error {
	if r.mode != swiftModeCheck {
		return fmt.Errorf("a project record in a %s run", r.mode)
	}
	if r.project != nil {
		return fmt.Errorf("a second project record")
	}
	r.project = record

	elapsed := time.Duration(record.ElapsedMilliseconds) * time.Millisecond
	line := fmt.Sprintf("package described in %s — %d Swift files in the package, %d of them ours",
		round(elapsed), record.FilesInPackage, record.FilesOurs)
	if record.FilesInScope != record.FilesOurs || record.ScopeDescription != "" {
		line += fmt.Sprintf(", %d in scope (%s)", record.FilesInScope, record.ScopeDescription)
	}
	fmt.Fprintln(r.out, line)

	r.report.graph = elapsed
	r.report.filesInScope = record.FilesInScope
	r.report.filesInProgram = record.FilesOurs
	return nil
}

// acceptUnreadable holds a file nothing checked, to be named with the excluded files and counted as a
// gap. Its place is fixed, after the project and before the first phase, so the files a run could not
// read are all known before any phase reports on the files it could.
func (r *swiftRun) acceptUnreadable(record *swiftUnreadableRecord) error {
	if r.mode != swiftModeCheck {
		return fmt.Errorf("an unreadable record in a %s run", r.mode)
	}
	if r.project == nil {
		return fmt.Errorf("an unreadable record before the project record")
	}
	if r.nextPhase > 0 {
		return fmt.Errorf("an unreadable record after phase %s, and contract 2 places them before the first phase", phaseOrder[r.nextPhase-1])
	}
	if record.File == "" {
		return fmt.Errorf("an unreadable record with no file")
	}
	r.unreadable = append(r.unreadable, *record)
	return nil
}

func (r *swiftRun) acceptFinding(record *swiftFindingRecord) error {
	if r.mode != swiftModeCheck {
		return fmt.Errorf("a finding record in a %s run", r.mode)
	}
	if record.File == "" || record.Line < 1 || record.Column < 1 {
		return fmt.Errorf("a finding with no position (file %q, line %d, column %d)", record.File, record.Line, record.Column)
	}
	r.findings++
	r.findingsInPhase++
	fmt.Fprintln(r.out, swiftFindingLine(record))
	return nil
}

// swiftFindingLine is one finding as one line, in the shape the TypeScript printers use.
//
// A compiler finding reads like tsc's `file:line:col - error TS2322: message`, with the warning group
// where the code goes, as swiftc writes it. A rule or format finding reads like a TypeScript rule
// finding, `[rule/messageId]` at the end. Newlines in the message are collapsed, for the reason
// printRuleDiagnostic gives.
func swiftFindingLine(record *swiftFindingRecord) string {
	position := fmt.Sprintf("%s:%d:%d", record.File, record.Line, record.Column)
	message := singleLineDescription(record.Message)
	if record.Source == "compiler" {
		line := fmt.Sprintf("%s - %s: %s", position, record.Severity, message)
		if record.Rule != "" {
			line += fmt.Sprintf(" [#%s]", record.Rule)
		}
		return line
	}
	return fmt.Sprintf("%s - %s [%s/%s]", position, message, record.Rule, record.MessageId)
}

func (r *swiftRun) acceptFix(record *swiftFixRecord) error {
	if r.mode != swiftModeCheck {
		return fmt.Errorf("a fix record in a %s run", r.mode)
	}
	// The engine's numbers fed to the summary the TypeScript run prints, so the line and its rules
	// (the refusal breakdown beside the refusal count, formatting counted apart from fixes) are the
	// ones edit.Summary already keeps.
	summary := edit.Summary{
		FilesConsidered:       record.FilesConsidered,
		FilesChanged:          record.FilesRewritten,
		FixesApplied:          record.FixesApplied,
		FixesRefused:          record.FixesRefused,
		RefusalsByReason:      record.RefusalsByReason,
		FilesTransformed:      record.FilesReformatted,
		FilesTransformSkipped: record.FilesNotFormatted,
		TransformSkipReasons:  record.NotFormattedReasons,
	}
	fmt.Fprintln(r.out, summary.String())
	fmt.Fprintf(r.out, "format scope: %s\n", record.FormatScope)
	return nil
}

func (r *swiftRun) acceptTypes(record *swiftTypesRecord) error {
	if r.mode != swiftModeCheck {
		return fmt.Errorf("a types record in a %s run", r.mode)
	}
	if record.Diagnostics != r.findingsInPhase {
		return fmt.Errorf("the types record counts %d diagnostics and %d finding records came with it", record.Diagnostics, r.findingsInPhase)
	}
	elapsed := time.Duration(record.ElapsedMilliseconds) * time.Millisecond
	fmt.Fprintf(r.out, "types: %d diagnostics over %d files in %s\n", record.Diagnostics, record.Files, round(elapsed))

	// The compiler checks the whole module whatever the scope, so a scoped run's types line covers more
	// than the scope does. Said, so the larger number does not read as a scope that leaked.
	if r.project != nil && record.Files != r.project.FilesInScope {
		fmt.Fprintf(r.out, "  types: covered the whole package, not only the %d files in scope\n", r.project.FilesInScope)
	}
	for _, file := range record.FilesWithoutRecord {
		fmt.Fprintf(r.out, "  types: no compiler record for %s, so its diagnostics are unknown\n", file)
	}
	r.filesWithoutRecord += len(record.FilesWithoutRecord)
	return nil
}

func (r *swiftRun) acceptLint(record *swiftLintRecord) error {
	if r.mode != swiftModeCheck {
		return fmt.Errorf("a lint record in a %s run", r.mode)
	}
	if record.Findings != r.findingsInPhase {
		return fmt.Errorf("the lint record counts %d findings and %d finding records came with it", record.Findings, r.findingsInPhase)
	}

	walkCost := fmt.Sprintf("in %s", round(time.Duration(record.ElapsedMilliseconds)*time.Millisecond))
	if record.ReusedFrom != "" {
		walkCost = fmt.Sprintf("walked by the %s phase (nothing was rewritten, so its findings still hold)", record.ReusedFrom)
	}
	fmt.Fprintf(r.out, "lint: %d findings — %d rules over %d files, %d nodes visited, %s\n",
		record.Findings, record.RulesRun, record.FilesWalked, record.NodesVisited, walkCost)

	writeWatchedAndQuietNote(r.out, record.RulesWatchedAndQuiet)
	silent := append([]string(nil), record.RulesSilent...)
	sort.Strings(silent)
	for _, name := range silent {
		// The engine reports one silent set where the TypeScript walk can tell "offered nothing" from
		// "declined everything". The sentence claims only what the record can back.
		fmt.Fprintf(r.out, "  note: rule %s listened to no files — nothing gave it a file, or it declined every one, so its silence says nothing about the tree\n", name)
	}
	for _, crash := range record.Crashes {
		writeCrashedNote(r.out, crash.File, crash.Error)
	}
	r.crashes += len(record.Crashes)

	scopedOff := make([]string, 0, len(record.RulesScopedOff))
	for name := range record.RulesScopedOff {
		scopedOff = append(scopedOff, name)
	}
	sort.Strings(scopedOff)
	for _, name := range scopedOff {
		writeScopedOffNote(r.out, name, record.RulesScopedOff[name])
	}
	unconfigured := append([]string(nil), record.RulesNotConfigured...)
	sort.Strings(unconfigured)
	for _, name := range unconfigured {
		writeUnconfiguredNote(r.out, name)
	}
	if record.ConfigNote != "" {
		fmt.Fprintf(r.out, "  %s\n", record.ConfigNote)
	}
	r.writeExcluded()
	return nil
}

// excludedNamedLimit is how many excluded files of one reason are named before they are counted.
const excludedNamedLimit = 5

// writeExcluded says which files the package description left out, once, under the lint line when
// there is one and above the phase line when there is not: a file nobody checked is a coverage fact
// whether or not lint got to run.
//
// Grouped by reason, and counted past a handful. The first run on ahraos-macos printed 107 lines of
// `vendored under Vendor/` between the lint line and the verdict, which buries the two lines a reader
// must act on under a hundred that say one thing. A few files are named, because one file left out
// for a surprising reason is the case worth seeing; a hundred for the same reason are one fact.
func (r *swiftRun) writeExcluded() {
	if r.excludedPrinted || r.project == nil {
		return
	}
	r.excludedPrinted = true

	filesByReason := map[string][]string{}
	reasons := []string{}
	add := func(file string, reason string) {
		if _, seen := filesByReason[reason]; !seen {
			reasons = append(reasons, reason)
		}
		filesByReason[reason] = append(filesByReason[reason], file)
	}
	for _, excluded := range r.project.Excluded {
		add(excluded.File, excluded.Reason)
	}
	// A file the engine could not read is printed the way an excluded file is, because to a reader it is
	// the same fact: a file nothing checked, and why.
	for _, unreadable := range r.unreadable {
		add(unreadable.File, "could not be read: "+unreadable.Error)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		files := filesByReason[reason]
		if len(files) > excludedNamedLimit {
			fmt.Fprintf(r.out, "  not checked: %d files (%s)\n", len(files), reason)
			continue
		}
		for _, file := range files {
			fmt.Fprintf(r.out, "  not checked: %s (%s)\n", file, reason)
		}
	}
}

func (r *swiftRun) acceptPhase(record *swiftPhaseRecord) error {
	if r.mode != swiftModeCheck {
		return fmt.Errorf("a phase record in a %s run", r.mode)
	}
	if r.nextPhase >= len(phaseOrder) || string(phaseOrder[r.nextPhase]) != record.Name {
		expected := "nothing more"
		if r.nextPhase < len(phaseOrder) {
			expected = string(phaseOrder[r.nextPhase])
		}
		return fmt.Errorf("a phase record for %q where the pipeline order expects %s", record.Name, expected)
	}
	outcome, known := swiftOutcomes[record.Outcome]
	if !known {
		return fmt.Errorf("phase %s has outcome %q, which is not one of ran, skipped, notReached, reused", record.Name, record.Outcome)
	}
	r.nextPhase++
	r.findingsInPhase = 0
	r.report.record(phaseOrder[r.nextPhase-1], outcome, time.Duration(record.ElapsedMilliseconds)*time.Millisecond, record.Detail)
	return nil
}

func (r *swiftRun) acceptRule(record *swiftRuleRecord) error {
	if r.mode != swiftModeRules && r.mode != swiftModeRulesEnabled {
		return fmt.Errorf("a rule record in a %s run", r.mode)
	}
	r.rules = append(r.rules, *record)
	return nil
}

func (r *swiftRun) acceptSummary(record *swiftSummaryRecord) error {
	if r.mode != swiftModeCheck {
		return fmt.Errorf("a summary record in a %s run", r.mode)
	}
	if record.Findings == nil || record.Complete == nil || record.ExitCode == nil {
		return fmt.Errorf("a summary missing findings, complete or exitCode")
	}
	if r.nextPhase != len(phaseOrder) {
		// The reason markRemainingNotReached exists: a phase absent from the report reads the same as
		// one the reporter forgot.
		return fmt.Errorf("a summary with no record for phase %s", phaseOrder[r.nextPhase])
	}
	if *record.Findings != r.findings {
		return fmt.Errorf("the summary counts %d findings and %d finding records arrived", *record.Findings, r.findings)
	}

	// The two views of completeness are compared in one direction only. Where the records show a gap
	// and the summary claims none, the engine is printing green over work it did not do, which is the
	// failure this tool exists to stop, and the run is refused. The other direction is allowed: an engine
	// that says it fell short is believed, even where no record shows the gap.
	//
	// A run with nothing to check skipped every phase because there was nothing for any of them to
	// look at, which recordNothingToCheck treats as a clean answer over zero files and so does this.
	namedGaps := r.filesWithoutRecord > 0 || r.crashes > 0 || len(r.unreadable) > 0
	gapInRecords := (record.NothingToCheck == "" && !r.report.checkedEverything()) || namedGaps
	if gapInRecords && *record.Complete {
		return fmt.Errorf("the summary calls the run complete, and its own records show what it did not check")
	}
	if !*record.Complete && r.report.checkedEverything() {
		if namedGaps {
			r.report.incompleteBeyondPhases = "the notes above name the files nothing checked"
		} else {
			r.report.incompleteBeyondPhases = "the Swift engine reported that it fell short without a record saying where"
		}
	}

	expectedExit := 0
	if r.findings > 0 || !*record.Complete {
		expectedExit = 1
	}
	if *record.ExitCode != expectedExit {
		return fmt.Errorf("the summary says exit %d, and %d findings in a run complete=%t means exit %d",
			*record.ExitCode, r.findings, *record.Complete, expectedExit)
	}

	r.summary = record
	r.writeExcluded()
	if record.NothingToCheck != "" {
		r.report.nothingToCheck = record.NothingToCheck
	}
	if r.project == nil {
		r.report.graphNotBuilt = true
	}
	r.report.Write(r.out)
	return nil
}

// finish decides the run's exit code once the engine has exited, from what it wrote and how it ended.
//
// Every path that is not a validated summary, or a listing mode's expected ending, is a failure that
// says nothing was checked. There is no path through here where a run that did not finish prints
// green.
func (r *swiftRun) finish(engineExit int, ended string) (int, error) {
	if engineExit == 2 {
		// The engine's own sentence is already on stderr, passed through untouched.
		return 1, fmt.Errorf("the Swift engine did not run, so nothing was checked")
	}

	switch r.mode {
	case swiftModeVersion:
		if engineExit != 0 || r.provenance == nil {
			return 1, fmt.Errorf("the Swift engine %s without stating its version", ended)
		}
		fmt.Fprintln(r.out, release.Current())
		fmt.Fprintf(r.out, "swift engine: %s %s (commit %s, %s, swift-syntax %s, swift-format %s)\n",
			r.provenance.Engine, r.provenance.Version, r.provenance.Commit,
			r.provenance.Toolchain, r.provenance.SwiftSyntax, r.provenance.SwiftFormat)
		if r.provenance.SourceTreeModified {
			fmt.Fprintln(r.out, "  the Swift engine was built from a modified tree, so no commit reproduces it")
		}
		return 0, nil

	case swiftModeRules, swiftModeRulesEnabled:
		if engineExit != 0 || r.provenance == nil {
			return 1, fmt.Errorf("the Swift engine %s before listing its rules", ended)
		}
		sort.Slice(r.rules, func(first, second int) bool { return r.rules[first].Name < r.rules[second].Name })
		for _, listed := range r.rules {
			if r.mode == swiftModeRulesEnabled {
				fmt.Fprintf(r.out, "%s\t%s\n", listed.Name, listed.Severity)
			} else {
				fmt.Fprintln(r.out, listed.Name)
			}
		}
		return 0, nil
	}

	if r.summary == nil {
		r.writeUnfinished(fmt.Sprintf("the Swift engine %s before finishing", ended))
		return 1, fmt.Errorf("the Swift engine %s without a summary, so nothing was checked", ended)
	}
	if engineExit != *r.summary.ExitCode {
		return 1, fmt.Errorf("the Swift engine %s, and its summary said it would exit %d", ended, *r.summary.ExitCode)
	}
	return *r.summary.ExitCode, nil
}

// writeUnfinished prints the phase line for a run that stopped early, every phase the engine never
// recorded marked not reached, so the last line a reader sees is never a clean bill of health.
func (r *swiftRun) writeUnfinished(reason string) {
	if r.mode != swiftModeCheck || r.summary != nil {
		return
	}
	for ; r.nextPhase < len(phaseOrder); r.nextPhase++ {
		r.report.record(phaseOrder[r.nextPhase], outcomeNotReached, 0, reason)
	}
	if r.project == nil {
		r.report.graphNotBuilt = true
	}
	r.report.Write(r.out)
}

// truncateForError keeps a refused line short enough to read in an error.
func truncateForError(line []byte) string {
	const limit = 200
	if len(line) <= limit {
		return string(line)
	}
	return string(line[:limit]) + "…"
}
