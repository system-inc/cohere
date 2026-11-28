package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/command_line/streams"
	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/release/packaging"
)

// contractVersion is the version of documentation/contract/Contract.md this front door speaks.
const contractVersion = 4

// versionReport is what `cohere --version` prints about cohere itself: its provenance, then the Swift
// contract it speaks, on every platform and every build, since it is true of each. The release reads
// that line from the cohere it stages and refuses a cohere-swift that will not speak the same version,
// so the number comes from the binary that ships rather than from a copy that could drift.
func versionReport() string {
	return fmt.Sprintf("%s\n  swift contract: %d", release.Current(), contractVersion)
}

// contractMode is which question a child run answers, and so which records it may carry.
//
// The listing modes end without a summary by design: `--version` prints one provenance record and
// `--rules` prints rule records, as the contract says. A run in the checking mode that ends without a
// summary is a crash. The mode is what lets one absence mean two different things without the front
// door guessing which.
type contractMode string

const (
	contractModeCheck        contractMode = "Check"
	contractModeVersion      contractMode = "Version"
	contractModeRules        contractMode = "Rules"
	contractModeRulesEnabled contractMode = "RulesEnabled"
)

// The records, as documentation/contract/Contract.md defines them. Fields the contract does not list are ignored by
// leaving them out of these types, which is what the contract asks of the front door.

type contractProvenanceRecord struct {
	Contract           *int   `json:"contract"`
	Program            string `json:"program"`
	Version            string `json:"version"`
	Commit             string `json:"commit"`
	SourceTreeModified bool   `json:"sourceTreeModified"`
	Toolchain          string `json:"toolchain"`
	SwiftSyntax        string `json:"swiftSyntax"`
	SwiftFormat        string `json:"swiftFormat"`
}

type contractProjectRecord struct {
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
		// Source is the rule that excluded the file, `path:line` relative to the project root, absent where no rule
		// did: contract 3 carries it as an optional field (#08cv2mp).
		Source string `json:"source"`
	} `json:"excluded"`
}

// contractUnreadableRecord is a file the child could not read, so nothing in it was checked. Contract 2
// made it a record: before, the child said only on stderr that it had fallen short, and the front door
// could believe it but not say which files.
type contractUnreadableRecord struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

type contractFindingRecord struct {
	Source    string `json:"source"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Severity  string `json:"severity"`
	Rule      string `json:"rule"`
	MessageId string `json:"messageId"`
	Message   string `json:"message"`
}

// contractUnusedRecord is one item of the --unused report. It carries a finding's place and words but is
// not a finding: the report is not a gate, so it is never counted and never moves the exit code, as the
// TypeScript unused report never does.
type contractUnusedRecord struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Rule      string `json:"rule"`
	MessageId string `json:"messageId"`
	Message   string `json:"message"`
	Subject   string `json:"subject"`
}

// contractUnusedCoverageRecord is what one rule of the --unused report looked at, so a report that checked
// nothing never reads like one that found nothing.
type contractUnusedCoverageRecord struct {
	Rule                string         `json:"rule"`
	FilesChecked        int            `json:"filesChecked"`
	FilesNotChecked     map[string]int `json:"filesNotChecked"`
	Checked             int            `json:"checked"`
	Skipped             map[string]int `json:"skipped"`
	Found               *int           `json:"found"`
	ElapsedMilliseconds int            `json:"elapsedMilliseconds"`
}

type contractFixRecord struct {
	FilesConsidered     int            `json:"filesConsidered"`
	FilesRewritten      int            `json:"filesRewritten"`
	FixesApplied        int            `json:"fixesApplied"`
	FixesRefused        int            `json:"fixesRefused"`
	RefusalsByReason    map[string]int `json:"refusalsByReason"`
	FilesReformatted    int            `json:"filesReformatted"`
	FilesNotFormatted   int            `json:"filesNotFormatted"`
	NotFormattedReasons map[string]int `json:"notFormattedReasons"`
	FormatScope         string         `json:"formatScope"`
	// ChangedFiles is every file the phase wrote, once each. Additive within contract 3: a child from
	// before it sends none, which is "not listed", never "no files changed", so it is a pointer.
	ChangedFiles *[]contractChangedFile `json:"changedFiles"`
	// NotWritten is every file the phase would have written and did not, since it is reached through a link to a real
	// path outside the root. Additive within contract 3: a child from before it sends none, and a pointer, as
	// ChangedFiles is, keeps that apart from a list that is empty.
	NotWritten *[]notWrittenFile `json:"notWritten"`
}

// contractChangedFile is one file the fix phase wrote: its fixes by the rule that made them, and whether the
// formatter changed it.
type contractChangedFile struct {
	File      string         `json:"file"`
	FixedBy   map[string]int `json:"fixedBy"`
	Formatted bool           `json:"formatted"`
}

type contractTypesRecord struct {
	Diagnostics         int      `json:"diagnostics"`
	Files               int      `json:"files"`
	ElapsedMilliseconds int      `json:"elapsedMilliseconds"`
	FilesWithoutRecord  []string `json:"filesWithoutRecord"`
	// WithheldInDependencyCgo and WithheldDependencyCgoDeclarations are cohere-go's, additive within contract
	// 3: what a run withheld because no cgo tool ran, the type errors in a dependency's cgo files by file, and
	// the count of dependency declarations given without bodies or initial values. cohere-swift sends neither,
	// which decodes as none.
	WithheldInDependencyCgo           []withheldCgoFile `json:"withheldInDependencyCgo"`
	WithheldDependencyCgoDeclarations int               `json:"withheldDependencyCgoDeclarations"`
	// CgoTranslation is cohere-go's too: how many packages the cgo tool translated, and how long that load took,
	// 0 and 0 on the load from source, which runs no cgo tool. cohere-swift sends none, which decodes as 0 and 0.
	CgoTranslation struct {
		Packages     int `json:"packages"`
		Milliseconds int `json:"milliseconds"`
	} `json:"cgoTranslation"`
	// CensusMissed is cohere-go's, additive within contract 3 (#yyv7nr2, #hd5bsqh): each file its load reached in
	// the workspace that its census never counted, with its package, absent when none. cohere-swift sends none.
	CensusMissed []censusMissedFile `json:"censusMissed"`
	// SdkIndexSeed is cohere-swift's, additive within contract 3: what the check of its standalone scripts did
	// with the toolchain's SDK index seed, its key and whether the run's store was seeded from it, absent when no
	// seed applied. cohere-go sends none.
	SdkIndexSeed *sdkIndexSeedUse `json:"sdkIndexSeed"`
	// Memory is cohere-go's, additive within contract 3: the memory limit its process ran under and where it
	// came from, printed in the account `--verbose` prints. cohere-swift sends none.
	Memory *goMemory `json:"memory"`
	// HeadersUnshared is cohere-go's, additive within contract 3 (#tb538na): why the run's listings did not
	// share each Go file's header through the child's copy of go/build, which happens only in a binary linked by a
	// Go release whose go/build is not the copy's. Absent when they shared them. cohere-swift sends none.
	HeadersUnshared string `json:"headersUnshared"`
}

type contractLintRecord struct {
	Findings int `json:"findings"`
	// TypeErrors is the type errors of a Go run's other builds, written in the lint phase as compiler finding records
	// beside the findings counted above, additive within contract 3 (#rw6tg3a). A child that writes none sends none.
	TypeErrors           int            `json:"typeErrors"`
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
	// Cache is what cohere-go's per-package findings cache did, additive within contract 3. cohere-swift
	// sends none, which decodes as nil and prints nothing.
	Cache *packageCacheUse `json:"cache"`
	// Builds is every build cohere-go checked when it checked more than one, the host's first, additive within
	// contract 3. A run of one build and cohere-swift send none, which decodes as nil and prints nothing.
	Builds []checkedBuild `json:"builds"`
	// BuildsAtOnce is how many of those builds beside the host's cohere-go ran side by side, additive within
	// contract 3 (#b2t9xmf): a record of how the run ran, which changes nothing it found. Sent only with Builds.
	BuildsAtOnce int `json:"buildsAtOnce"`
	// BuildsWidthLiveBytes is the host's live heap BuildsAtOnce was decided from, additive within contract 3, absent
	// when the decision needed none (#qwb6ns8).
	BuildsWidthLiveBytes uint64 `json:"buildsWidthLiveBytes"`
	// BuildsWidthFromHostLive is whether that heap alone decided BuildsAtOnce, every build's share at the share gate
	// low, additive within contract 3 (#ghb3rcn), absent when the first build's holding was measured instead.
	BuildsWidthFromHostLive bool `json:"buildsWidthFromHostLive"`
	// OffReasons is why the settings turn each rule off, for each off cohere-go's settings explain, additive
	// within contract 3 (#v11k0g2). Nil from a child that never explains its offs, cohere-swift's, so only an
	// child that sends it can say a rule is off with no reason given.
	OffReasons map[string]string `json:"offReasons"`
}

type contractPhaseRecord struct {
	Name                string `json:"name"`
	Outcome             string `json:"outcome"`
	ElapsedMilliseconds int    `json:"elapsedMilliseconds"`
	Detail              string `json:"detail"`
}

type contractRuleRecord struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
}

// contractInputsRecord is every path a check run's verdict depends on, absolute and sorted, which the Go and Swift
// children write once before their summary: each file whose bytes it read, each directory whose listing decided which
// packages or files exist, and each path whose absence the run depended on, such as a go.work in an ancestor.
// Additive within contract 3, and the run cache's proof for a child's run: the front door cannot watch what `go
// list`, SwiftPM or the child read in processes of their own, so the child says, and a run that ends without saying
// is never recorded.
type contractInputsRecord struct {
	Files       []string `json:"files"`
	Directories []string `json:"directories"`
	Absent      []string `json:"absent"`
	// TrackedIgnored is each git index among Files the child read only for what it decides of a walk, which the run
	// cache keys on that rather than on the index's bytes (#m1daxxk). Absent from a record of a child before it.
	TrackedIgnored []contractTrackedIgnored `json:"trackedIgnored"`
}

// contractTrackedIgnored is one index a child read for a walk: the index, the root of its repository, and the
// directory walked relative to that root, slash separated, "" for the root.
type contractTrackedIgnored struct {
	Index string `json:"index"`
	Root  string `json:"root"`
	Scope string `json:"scope"`
}

type contractSummaryRecord struct {
	Findings *int  `json:"findings"`
	Complete *bool `json:"complete"`
	ExitCode *int  `json:"exitCode"`
	// UnguardedWorkspaceSums is cohere-go's, additive within contract 3 (#34r3v1y): each go.work.sum one of its go
	// list commands ran beside with no side copy of the go.work, so go may have rewritten it.
	UnguardedWorkspaceSums []string `json:"unguardedWorkspaceSums"`
}

// contractOutcomes maps the contract's camelCase outcomes onto the ones pipeline_report.go prints.
var contractOutcomes = map[string]phaseOutcome{
	"ran":        outcomeRan,
	"skipped":    outcomeSkipped,
	"notReached": outcomeNotReached,
	"reused":     outcomeReused,
}

// contractRun renders one child run's records as they arrive, through the printers a TypeScript run
// uses, and refuses any stream that breaks the contract.
//
// Every refusal is an error rather than a skipped line, for the reason the contract gives: a skipped
// record is a finding nobody sees. And every count the stream states twice is compared, because two
// numbers that should agree and do not are a defect in one of them, and printing either would be
// printing a number nobody can vouch for.
//
// out is shown after each finding and each phase's line, so a person sees what the child found as it arrives,
// and a slow phase's line before its wait rather than when the run ends.
type contractRun struct {
	out  *bufio.Writer
	mode contractMode

	// language is the words this run prints for the child, so a sentence never names a language the run
	// is not checking.
	language contractLanguage

	// details is `--coverage`: name every rule once under its coverage fact rather than only counting.
	// A front-door flag, never forwarded: the child's records already carry what it names.
	details bool

	// swiftSourceCommit is the commit whose Swift sources the child is guaranteed to be built from,
	// or empty when nothing vouches for it, and only cohere-swift's version lines print it. Set only
	// when the child came through the launcher's committed path, whose cache key is the object ids of
	// that commit's Swift inputs. The child itself says `commit dev` and must: one cached child serves
	// every commit with identical Swift inputs, so a commit baked into it would name whichever commit
	// built it first, true of the build and false of the run. The front door knows the run.
	swiftSourceCommit string

	// requested is which phases the caller asked for, by the flags the TypeScript run reads the same way
	// (contractRequestedPhases). A child that reports one of them skipped as not requested is refused: it
	// would turn a phase that was asked for and never ran into a run that looks complete. Nil, as in a test
	// that renders a stream with no command line, checks nothing.
	requested map[phaseName]bool

	// childEnvironment is what the child's process gets in its environment beyond this one's, each
	// `NAME=value`: cohere-go's child mark (goChildVariable). Empty for a child that needs
	// nothing more, which inherits this process's environment as it is.
	childEnvironment []string

	// yieldedDirectories are the directories of the Swift projects below this one that run on their own,
	// symbolic links resolved, when this run is one project of several. Their runs account for their files,
	// so the excluded files under them are left out of this run's notes and its gap. Nil checks nothing, and
	// only a Swift run sets it (see discovery.swiftYield).
	yieldedDirectories []string

	provenance *contractProvenanceRecord
	project    *contractProjectRecord
	summary    *contractSummaryRecord
	report     *pipelineReport
	rules      []contractRuleRecord

	// inputs is what the child said the run read, nil until its inputs record arrives. See goRunCacheProof.
	inputs *contractInputsRecord

	// timingRequested is `--timing` on a Go run, which the child answers with one timing record. A run that did not
	// ask is sent none, so a timing record there is a broken stream. timingReceived says the one record came.
	timingRequested bool
	timingReceived  bool
	// timelineRequested is `--timeline` on a Go run, which the child answers with one timeline record, the last
	// before the summary. A run that did not ask is sent none, so a timeline record there is a broken stream.
	// timelineReceived says the one record came.
	timelineRequested bool
	timelineReceived  bool
	// goMemory is the limit a Go run's child held its heap under, from its types record, kept until the lint record
	// says how many builds ran at once beside it, or the summary says none will (sayGoMemory).
	goMemory *goMemory

	// notLinted is each file a Go build compiled that no rule ran on, by path, with why (checkedBuild.NotLinted), from
	// the lint record, so writeExcluded names it as not checked rather than as checked by that build (#ytnvt8k).
	notLinted map[string]string

	// filesRewritten is how many files the fix phase wrote, so a run that rewrote the tree it checked is never
	// recorded as a verdict about that tree.
	filesRewritten int

	// cgoTranslated is whether the load ran the cgo tool, which reads C headers and a compiler no inputs record can
	// name, so such a run is never recorded. See goInputsUnnameable.
	cgoTranslated bool

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
	unreadable         []contractUnreadableRecord

	// censusMissed is each file the types record says the load reached and the census never counted
	// (censusMissedTheLoad).
	censusMissed []censusMissedFile

	// notWritten are the files the fix record names as not written, each reached through a link outside the root:
	// already printed as notes, and a gap that makes the run incomplete.
	notWritten []notWrittenFile

	// What the types records say a Go run withheld because no cgo tool ran, already printed as notes and held
	// for the summary's gaps. Neither makes the run incomplete: what was withheld is a dependency's, not the
	// project's to fix.
	withheldInDependencyCgo           []withheldCgoFile
	withheldDependencyCgoDeclarations int

	excludedPrinted bool

	// The --unused report: items held until their rule's coverage record says how many there are, so the
	// section prints with its count first, in the TypeScript report's shape. unusedCoverage counts the
	// coverage records, which an unused phase that ran must have.
	unusedHeadingPrinted bool
	unusedPending        []contractUnusedRecord
	unusedCoverage       int

	// duplicatesRequested is `--duplicates`, which only cohere-go takes, and duplicatesWritten whether its one
	// duplicates record came: a run that asked and ended without it is refused, as a report that never ran must not
	// read as one that found nothing.
	duplicatesRequested bool
	duplicatesWritten   bool

	// duplicatesUnanswered is why a child with no duplicates report did not run one under `--duplicates`, printed
	// as a report that did not run before the run's end, and empty otherwise.
	duplicatesUnanswered string
}

// newContractRun starts rendering one language's run, naming the step nothing can skip by what the child
// describes first, its package, module or workspace.
//
// The run's `--no-cache` is the front door's flag, passed to the child through its arguments, so the report reads
// it here: the summary's `cache.off` and the account's note say it, as a TypeScript run's do, in the words of what
// the child keeps (contractLanguage.CacheOffDetail). Without it every Swift summary said the cache was on under
// `--no-cache`, a report that lies about what ran.
func newContractRun(
	out *bufio.Writer,
	mode contractMode,
	language contractLanguage,
	rootNote string,
	processStart time.Time,
) *contractRun {
	return &contractRun{
		out:      out,
		mode:     mode,
		language: language,
		report: &pipelineReport{
			processStart:          processStart,
			rootNote:              rootNote,
			graphLabel:            language.Container,
			graphNotBuiltSentence: fmt.Sprintf("no %s was described and no phase ran", language.Container),
			cacheOff:              cacheOff,
			cacheOffDetail:        language.CacheOffDetail,
		},
	}
}

// accept renders one line of the child's stdout.
func (run *contractRun) accept(line []byte) error {
	var envelope struct {
		Kind string `json:"kind"`
	}
	if decodeError := json.Unmarshal(line, &envelope); decodeError != nil {
		return fmt.Errorf("a line that is not a record: %q (%w)", truncateForError(line), decodeError)
	}

	if run.summary != nil {
		return fmt.Errorf("a %q record after the summary, which is always last", envelope.Kind)
	}
	if run.provenance == nil && envelope.Kind != "provenance" {
		return fmt.Errorf("the first record was %q, and provenance is always first", envelope.Kind)
	}
	// A second timeline record is acceptTimeline's to refuse by name.
	if run.timelineReceived && envelope.Kind != "summary" && envelope.Kind != "timeline" {
		return fmt.Errorf("a %q record after the timeline record, which is the last before the summary",
			envelope.Kind)
	}

	switch envelope.Kind {
	case "provenance":
		return decodeRecord(line, &contractProvenanceRecord{}, run.acceptProvenance)
	case "project":
		return decodeRecord(line, &contractProjectRecord{}, run.acceptProject)
	case "unreadable":
		return decodeRecord(line, &contractUnreadableRecord{}, run.acceptUnreadable)
	case "finding":
		return decodeRecord(line, &contractFindingRecord{}, run.acceptFinding)
	case "fix":
		return decodeRecord(line, &contractFixRecord{}, run.acceptFix)
	case "types":
		return decodeRecord(line, &contractTypesRecord{}, run.acceptTypes)
	case "lint":
		return decodeRecord(line, &contractLintRecord{}, run.acceptLint)
	case "phase":
		return decodeRecord(line, &contractPhaseRecord{}, run.acceptPhase)
	case "rule":
		return decodeRecord(line, &contractRuleRecord{}, run.acceptRule)
	case "unused":
		return decodeRecord(line, &contractUnusedRecord{}, run.acceptUnused)
	case "unusedCoverage":
		return decodeRecord(line, &contractUnusedCoverageRecord{}, run.acceptUnusedCoverage)
	case "duplicates":
		return decodeRecord(line, &duplicate_functions.Report{}, run.acceptDuplicates)
	case "inputs":
		return decodeRecord(line, &contractInputsRecord{}, run.acceptInputs)
	case "timing":
		return decodeRecord(line, &contract.TimingRecord{}, run.acceptTiming)
	case "timeline":
		return decodeRecord(line, &contract.TimelineRecord{}, run.acceptTimeline)
	case "summary":
		return decodeRecord(line, &contractSummaryRecord{}, run.acceptSummary)
	default:
		// An unknown kind is refused rather than ignored: it is a record this front door cannot render,
		// so whatever it carried would reach nobody.
		return fmt.Errorf("a record of unknown kind %q", envelope.Kind)
	}
}

// decodeRecord decodes a line into its record type and hands it on.
func decodeRecord[Record any](line []byte, record *Record, accept func(*Record) error) error {
	if decodeError := json.Unmarshal(line, record); decodeError != nil {
		return fmt.Errorf("a record that does not decode: %q (%w)", truncateForError(line), decodeError)
	}
	return accept(record)
}

func (run *contractRun) acceptProvenance(record *contractProvenanceRecord) error {
	if run.provenance != nil {
		return fmt.Errorf("a second provenance record")
	}
	// Checked here as well as by the child against `--contract`, because the flag and the record are
	// two independent ways for the versions to disagree, and the contract asks for both.
	if record.Contract == nil || *record.Contract != contractVersion {
		spoken := "none"
		if record.Contract != nil {
			spoken = fmt.Sprint(*record.Contract)
		}
		return fmt.Errorf(
			"the child speaks contract %s and this front door speaks contract %d",
			spoken,
			contractVersion,
		)
	}
	run.provenance = record
	run.report.contractSourceTreeModified = record.SourceTreeModified
	return nil
}

func (run *contractRun) acceptProject(record *contractProjectRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("a project record in a %s run", run.mode)
	}
	if run.project != nil {
		return fmt.Errorf("a second project record")
	}
	run.project = record

	elapsed := time.Duration(record.ElapsedMilliseconds) * time.Millisecond
	line := fmt.Sprintf("%s described in %s: %d %s in the %s, %d of them ours",
		run.language.Container, round(elapsed), record.FilesInPackage, run.language.FileNoun, run.language.Container,
		record.FilesOurs)
	if record.FilesInScope != record.FilesOurs || record.ScopeDescription != "" {
		line += fmt.Sprintf(", %d in scope (%s)", record.FilesInScope, record.ScopeDescription)
	}
	// The invocation's, as a TypeScript run's graph line is: it says what this run took to describe the project,
	// which a replay did not spend (see invocationTag).
	printInvocation(accountOutput(run.out), "%s\n", line)
	streams.Show(run.out)
	// Checked is what a phase examined, so it starts at none: the types and lint records set it as they come. A run
	// whose files no phase reached reads none checked beside the gap that names them (#1hva1ya).
	activeSummary.FilesInScope, activeSummary.FilesChecked = record.FilesInScope, 0
	activeSummary.Gaps.NothingToCheck = record.FilesInScope == 0

	run.report.graph = elapsed
	run.report.filesInScope = record.FilesInScope
	run.report.filesInProgram = record.FilesOurs
	return nil
}

// acceptUnreadable holds a file nothing checked, to be named with the excluded files and counted as a
// gap. Its place is fixed, after the project and before the first phase, so the files a run could not
// read are all known before any phase reports on the files it could.
func (run *contractRun) acceptUnreadable(record *contractUnreadableRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("an unreadable record in a %s run", run.mode)
	}
	if run.project == nil {
		return fmt.Errorf("an unreadable record before the project record")
	}
	if run.nextPhase > 0 {
		return fmt.Errorf(
			"an unreadable record after phase %s, and contract 2 places them before the first phase",
			phaseOrder[run.nextPhase-1],
		)
	}
	if record.File == "" {
		return fmt.Errorf("an unreadable record with no file")
	}
	run.unreadable = append(run.unreadable, *record)
	return nil
}

func (run *contractRun) acceptFinding(record *contractFindingRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("a finding record in a %s run", run.mode)
	}
	if record.File == "" || record.Line < 1 || record.Column < 1 {
		return fmt.Errorf(
			"a finding with no position (file %q, line %d, column %d)",
			record.File,
			record.Line,
			record.Column,
		)
	}
	run.findings++
	run.findingsInPhase++
	// A compiler finding is a type error, as the TypeScript run counts its own; the rest are findings.
	// The contract spells the severities error and warning; the view spells a warning warn, as the settings do.
	severity := "error"
	if record.Severity == "warning" {
		severity = "warn"
	}
	rule, messageId := record.Rule, record.MessageId
	if record.Source == "compiler" {
		activeSummary.TypeErrors++
		if rule != "" {
			rule = "#" + rule
		} else {
			rule = run.language.CompilerLabel
		}
	} else {
		activeSummary.Findings++
	}
	printFinding(run.out,
		runFinding{
			Path:      record.File,
			Line:      record.Line,
			Column:    record.Column,
			Severity:  severity,
			Rule:      rule,
			MessageId: messageId,
			Message:   singleLineDescription(record.Message),
		},
		contractFindingLine(record)+"\n")
	return nil
}

// contractFindingLine is one finding as one line, in the shape the TypeScript printers use.
//
// A compiler finding reads like tsc's `file:line:col - error TS2322: message`, with the compiler's group
// where the code goes, as swiftc writes its warning groups. A rule or format finding reads like a
// TypeScript rule finding, `[rule/messageId]` at the end. Newlines in the message are collapsed, for the
// reason printRuleDiagnostic gives.
func contractFindingLine(record *contractFindingRecord) string {
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

// unusedPlace refuses an --unused report record anywhere but between the lint phase record and the
// unused one, where the contract puts the report.
func (run *contractRun) unusedPlace(kind string) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("an %s record in a %s run", kind, run.mode)
	}
	if run.nextPhase >= len(phaseOrder) || phaseOrder[run.nextPhase] != phaseUnused {
		return fmt.Errorf("an %s record outside the unused phase, which comes after lint", kind)
	}
	return nil
}

func (run *contractRun) acceptUnused(record *contractUnusedRecord) error {
	if unusedPlaceError := run.unusedPlace("unused"); unusedPlaceError != nil {
		return unusedPlaceError
	}
	if record.File == "" || record.Line < 1 || record.Column < 1 || record.Rule == "" || record.Subject == "" {
		return fmt.Errorf(
			"an unused record missing its file, position, rule or subject (file %q, line %d, column %d, rule %q)",
			record.File,
			record.Line,
			record.Column,
			record.Rule,
		)
	}
	run.unusedPending = append(run.unusedPending, *record)
	return nil
}

// acceptUnusedCoverage prints one rule's section of the report: its items, counted and sorted, then what
// it looked at and what it could not.
func (run *contractRun) acceptUnusedCoverage(record *contractUnusedCoverageRecord) error {
	if unusedPlaceError := run.unusedPlace("unusedCoverage"); unusedPlaceError != nil {
		return unusedPlaceError
	}
	if record.Rule == "" || record.Found == nil {
		return fmt.Errorf("an unusedCoverage record missing its rule or found count")
	}
	items := []contractUnusedRecord{}
	for _, pending := range run.unusedPending {
		if pending.Rule == record.Rule {
			items = append(items, pending)
		}
	}
	if len(items) != len(run.unusedPending) {
		return fmt.Errorf("unused records of another rule arrived before the coverage record for %s", record.Rule)
	}
	if *record.Found != len(items) {
		return fmt.Errorf(
			"the unusedCoverage record for %s counts %d found and %d unused records came with it",
			record.Rule,
			*record.Found,
			len(items),
		)
	}
	run.unusedPending = nil
	run.unusedCoverage++

	// The dashes here and in the items below are escaped, not changed: the report is in the TypeScript unused
	// report's shape (internal/unused_exports/report.go, documentation/contract/Contract.md), so the two print the same
	// bytes.
	if !run.unusedHeadingPrinted {
		run.unusedHeadingPrinted = true
		fmt.Fprintf(run.out, "\nunused: a report, not a gate \u2014 nothing here fails a build\n")
	}
	// File, line, then column, so two items on one line print in the same order on every run.
	sort.SliceStable(items, func(first, second int) bool {
		if items[first].File != items[second].File {
			return items[first].File < items[second].File
		}
		if items[first].Line != items[second].Line {
			return items[first].Line < items[second].Line
		}
		return items[first].Column < items[second].Column
	})
	if len(items) > 0 {
		fmt.Fprintf(run.out, "\n  %s \u2014 %d\n", swiftUnusedSection(record.Rule), len(items))
		for _, item := range items {
			fmt.Fprintf(run.out, "    %s:%d:%d \u2014 %s\n", item.File, item.Line, item.Column, item.Subject)
		}
	}
	fmt.Fprintf(
		run.out,
		"  looked at %d files and %d %s (%s)\n",
		record.FilesChecked,
		record.Checked,
		swiftUnusedNoun(record.Rule),
		record.Rule,
	)
	for _, reason := range sortedKeys(record.FilesNotChecked) {
		fmt.Fprintf(
			run.out,
			"  not checked for unused %s: %d files (%s)\n",
			swiftUnusedNoun(record.Rule),
			record.FilesNotChecked[reason],
			reason,
		)
	}
	for _, reason := range sortedKeys(record.Skipped) {
		fmt.Fprintf(
			run.out,
			"  never reported: %d %s (%s)\n",
			record.Skipped[reason],
			swiftUnusedNoun(record.Rule),
			reason,
		)
	}
	return nil
}

// swiftUnusedWords holds each known rule's heading and the noun for what it judges, in the TypeScript
// report's words. A rule not listed here prints under its own id and counts "items", so a new rule never
// reads under another rule's heading.
var swiftUnusedWords = map[string]struct{ section, noun string }{
	"cohere-swift/correctness-no-unused-import":      {"imports nothing uses", "imports"},
	"cohere-swift/correctness-no-unused-declaration": {"declarations nothing uses", "declarations"},
}

// swiftUnusedSection is the heading of one rule's items.
func swiftUnusedSection(rule string) string {
	if words, known := swiftUnusedWords[rule]; known {
		return words.section
	}
	return rule
}

// swiftUnusedNoun names what a rule judges, for its coverage line.
func swiftUnusedNoun(rule string) string {
	if words, known := swiftUnusedWords[rule]; known {
		return words.noun
	}
	return "items"
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (run *contractRun) acceptFix(record *contractFixRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("a fix record in a %s run", run.mode)
	}
	// The child's numbers fed to the summary the TypeScript run prints, so the line and its rules
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
	// True of the tree and produced by a phase, as a TypeScript run's fix line is, so a replay says which run they
	// came from (see provenanceTag).
	printProvenance(accountOutput(run.out), "%s\n", summary.String())
	printProvenance(accountOutput(run.out), "format scope: %s\n", record.FormatScope)
	run.filesRewritten += record.FilesRewritten
	// Each by name with where it really is, as the files nothing checked are named, since a gap nobody can find is
	// half a gap. The footer counts them.
	if record.NotWritten != nil {
		for _, file := range *record.NotWritten {
			fmt.Fprintf(accountOutput(run.out), "  not written: %s → %s\n", file.File, file.RealPath)
		}
		run.notWritten = append(run.notWritten, *record.NotWritten...)
	}

	// A child from before the list sends none, and the run then states the counts alone: the footer
	// leaves out a cohered count it was not given, rather than print a zero it does not know.
	if record.ChangedFiles == nil {
		streams.Show(run.out)
		return nil
	}
	changed, changedFilesError := run.changedFiles(*record.ChangedFiles, record.FilesRewritten)
	if changedFilesError != nil {
		return changedFilesError
	}
	activeSummary.Changed = changed
	printChangedFiles(run.out, changed)
	streams.Show(run.out)
	return nil
}

// changedFiles turns the fix record's list into the summary's, each path relative to the project root as a
// TypeScript run's are. It refuses a list whose length differs from the count the record states, a file
// named twice, and a file that neither a fix nor the formatter changed, since "N cohered" must be the list.
func (run *contractRun) changedFiles(files []contractChangedFile, rewritten int) ([]changedFile, error) {
	if len(files) != rewritten {
		return nil, fmt.Errorf("the fix record counts %d files rewritten and lists %d", rewritten, len(files))
	}
	root := ""
	if run.project != nil {
		root = run.project.Root
	}
	seen := map[string]bool{}
	changed := make([]changedFile, 0, len(files))
	for _, file := range files {
		if seen[file.File] {
			return nil, fmt.Errorf("the fix record lists %s twice", file.File)
		}
		seen[file.File] = true
		if len(file.FixedBy) == 0 && !file.Formatted {
			return nil, fmt.Errorf("the fix record lists %s as rewritten by no fix and not by the formatter", file.File)
		}
		path := file.File
		if relative, relativePathError := filepath.Rel(
			root,
			file.File,
		); root != "" && relativePathError == nil && !strings.HasPrefix(relative, "..") {
			path = relative
		}
		changed = append(changed, changedFile{
			Path:      path,
			Fixed:     len(file.FixedBy) > 0,
			Formatted: file.Formatted,
			FixedBy:   file.FixedBy,
		})
	}
	return changed, nil
}

func (run *contractRun) acceptTypes(record *contractTypesRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("a types record in a %s run", run.mode)
	}
	if record.Diagnostics != run.findingsInPhase {
		return fmt.Errorf(
			"the types record counts %d diagnostics and %d finding records came with it",
			record.Diagnostics,
			run.findingsInPhase,
		)
	}
	elapsed := time.Duration(record.ElapsedMilliseconds) * time.Millisecond
	// The invocation's, as a TypeScript run's types line is, since it carries what this run spent.
	printInvocation(
		accountOutput(run.out),
		"types: %d diagnostics over %d files in %s\n",
		record.Diagnostics,
		record.Files,
		round(elapsed),
	)

	// The compiler checks the whole module whatever the scope, so a scoped run's types line covers more
	// than the scope does. Said, so the larger number does not read as a scope that leaked.
	if run.project != nil && record.Files != run.project.FilesInScope {
		fmt.Fprintf(accountOutput(run.out), "  types: covered the whole %s, not only the %d files in scope\n",
			run.language.Container, run.project.FilesInScope)
	}
	for _, file := range record.FilesWithoutRecord {
		fmt.Fprintf(
			accountOutput(run.out),
			"  types: no compiler record for %s, so its diagnostics are unknown\n",
			file,
		)
	}
	run.filesWithoutRecord += len(record.FilesWithoutRecord)
	run.censusMissed = record.CensusMissed
	// The compiler examined every file it left a record for, at most the files in scope, since it checks the whole
	// container whatever the scope. A file with no record it did not examine (#1hva1ya).
	examined := max(record.Files-len(record.FilesWithoutRecord), 0)
	if run.project != nil {
		examined = min(examined, run.project.FilesInScope)
	}
	activeSummary.FilesChecked = max(activeSummary.FilesChecked, examined)

	// Said, so a cold run that took the toolchain's SDK index units never reads like one that indexed them: in the
	// account `--verbose` prints and in `--json`'s summary, never in the default line, as #f6pqraj ruled.
	if seed := record.SdkIndexSeed; seed != nil {
		if seed.Hit {
			fmt.Fprintf(accountOutput(run.out), "  types: SDK index units from the toolchain seed %s\n", seed.Key)
		} else {
			fmt.Fprintf(accountOutput(run.out),
				"  types: no toolchain seed %s held the SDK's index units, so they were indexed afresh\n", seed.Key)
		}
		activeSummary.Swift = &swiftSummary{SdkIndexSeed: seed}
	}

	// Said once the lint record says how many builds ran at once under it (sayGoMemory).
	run.goMemory = record.Memory
	// The invocation's too: a binary whose go/build is not the child's copy reads the same listing at more CPU, and
	// a reader timing a run needs to know which binary it timed (#fnsk1jv).
	if record.HeadersUnshared != "" {
		activeSummary.HeadersUnshared = record.HeadersUnshared
		printInvocation(accountOutput(run.out), "  headers: not shared, %s\n", record.HeadersUnshared)
	}

	// Said, so a run that withheld what no cgo tool could settle never reads like one with nothing to withhold:
	// counted under the types line, then each file by name, since a withheld file nobody can find is half said.
	if withheld := withheldCgoSentence(
		record.WithheldInDependencyCgo,
		record.WithheldDependencyCgoDeclarations,
	); withheld != "" {
		fmt.Fprintf(accountOutput(run.out), "  types: %s\n", withheld)
	}
	for _, file := range record.WithheldInDependencyCgo {
		fmt.Fprintf(accountOutput(run.out), "  types: withheld %s in %s (package %s)\n",
			counted(file.Errors, "type error", "type errors"), file.File, file.Package)
	}
	run.withheldInDependencyCgo = append(run.withheldInDependencyCgo, record.WithheldInDependencyCgo...)
	translated := record.CgoTranslation.Packages > 0 || record.CgoTranslation.Milliseconds > 0
	run.cgoTranslated = run.cgoTranslated || translated
	run.withheldDependencyCgoDeclarations += record.WithheldDependencyCgoDeclarations
	streams.Show(run.out)
	return nil
}

// withheldCgoSentence counts what a Go run withheld because no cgo tool ran, in one clause, or is empty when it
// withheld nothing: the type errors and the files they are in, then the declarations given without bodies or
// initial values, each said only when there are some.
func withheldCgoSentence(files []withheldCgoFile, declarations int) string {
	typeErrors := 0
	for _, file := range files {
		typeErrors += file.Errors
	}
	const reason = ", since no cgo tool ran"
	switch {
	case len(files) > 0 && declarations > 0:
		return fmt.Sprintf("withheld %s in %s of dependencies with cgo, and %s given without bodies or initial values",
			counted(typeErrors, "type error", "type errors"), counted(len(files), "file", "files"),
			counted(declarations, "of their declarations was", "of their declarations were")) + reason
	case len(files) > 0:
		return fmt.Sprintf("withheld %s in %s of dependencies with cgo",
			counted(typeErrors, "type error", "type errors"), counted(len(files), "file", "files")) + reason
	case declarations > 0:
		verb := "were"
		if declarations == 1 {
			verb = "was"
		}
		return fmt.Sprintf("%s of dependencies with cgo %s given without bodies or initial values",
			counted(declarations, "declaration", "declarations"), verb) + reason
	}
	return ""
}

func (run *contractRun) acceptLint(record *contractLintRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("a lint record in a %s run", run.mode)
	}
	if record.Findings+record.TypeErrors != run.findingsInPhase {
		return fmt.Errorf(
			"the lint record counts %d findings and %d type errors of other builds, and %d finding records came with it",
			record.Findings,
			record.TypeErrors,
			run.findingsInPhase,
		)
	}

	walkCost := fmt.Sprintf("in %s", round(time.Duration(record.ElapsedMilliseconds)*time.Millisecond))
	if record.ReusedFrom != "" {
		walkCost = fmt.Sprintf(
			"walked by the %s phase (nothing was rewritten, so its findings still hold)",
			record.ReusedFrom,
		)
	}
	// The dash is escaped, not changed: it is the TypeScript run's lint line, which internal/differential finds
	// by it. See writeLintReport. The invocation's, as that line is, since it carries what this run spent.
	printInvocation(accountOutput(run.out),
		"lint: %d findings \u2014 %d rules over %d files, %d nodes visited, %s\n",
		record.Findings, record.RulesRun, record.FilesWalked, record.NodesVisited, walkCost)
	// The invocation's, as the lint line above is: what this run's cache did, which a replay of it did not do.
	if record.Cache != nil {
		printInvocation(accountOutput(run.out), "  lint: %s\n", record.Cache.line())
		activeSummary.PackageCache = record.Cache
	}
	// The tree's, unlike the cache's line: the builds are what the verdict was merged from, so a replay that repeats
	// the verdict says them too, from the run that checked them.
	for _, build := range record.Builds {
		printProvenance(accountOutput(run.out), "  lint: %s\n", build.line())
	}
	activeSummary.Builds = record.Builds
	activeSummary.BuildsAtOnce = record.BuildsAtOnce
	run.sayGoMemory(record.BuildsAtOnce)
	activeSummary.BuildsWidthLiveBytes = record.BuildsWidthLiveBytes
	activeSummary.BuildsWidthFromHostLive = record.BuildsWidthFromHostLive
	for _, build := range record.Builds {
		for _, file := range build.NotLinted {
			if run.notLinted == nil {
				run.notLinted = map[string]string{}
			}
			run.notLinted[file] = build.NotLintedReason
		}
	}
	activeSummary.Rules = record.RulesRun
	activeSummary.FilesInScope, activeSummary.FilesChecked = record.FilesWalked, record.FilesWalked
	activeSummary.Nodes = record.NodesVisited
	activeSummary.Gaps.CrashedFiles += len(record.Crashes)

	// The same coverage block a TypeScript run prints, from the same writer, so one fact reads the same
	// whichever child states it: one counted line whose terms add up, crashes in full, and each rule
	// named once behind --coverage.
	summary, classifyContractCoverageError := classifyContractCoverage(record, run.language)
	if classifyContractCoverageError != nil {
		return classifyContractCoverageError
	}
	writeCoverage(accountOutput(run.out), summary, run.details)
	run.crashes += len(record.Crashes)
	// Named for the human view and `--json` as a TypeScript run's are, from the same crashes the coverage notes
	// named, so every view says the child's words, and appended as the count is added to, so the list stays as
	// long as the count.
	activeSummary.Gaps.Crashes = append(activeSummary.Gaps.Crashes, summary.Crashes...)
	printCrashGroups(run.out, summary.Crashes)

	if record.ConfigNote != "" {
		fmt.Fprintf(accountOutput(run.out), "  %s\n", record.ConfigNote)
	}
	run.writeExcluded()
	streams.Show(run.out)
	return nil
}

// classifyContractCoverage puts every rule the lint record accounts for into exactly one category.
//
// The record names its silent rules and only counts the rest: rulesRun is every rule, of which
// rulesWatchedAndQuiet listened and reported nothing and rulesSilent listened to no file, so what
// remains found something. A silent rule is described by why it was silent when the record says (not
// configured, or off by the config), and otherwise by the one sentence the record can back: the child
// reports one silent set where the TypeScript walk can tell "offered nothing" from "declined
// everything", so it claims neither.
//
// A record whose parts add up to more than its whole is refused, for the reason every count stated
// twice is compared: the counted line would print arithmetic nobody can vouch for.
//
// Each crash carries the child's own sentence and language's name for the child, so it reads as that child's
// report and never as a panic of cohere's (see runCrash.message).
func classifyContractCoverage(record *contractLintRecord, language contractLanguage) (coverageSummary, error) {
	notConfigured := make(map[string]bool, len(record.RulesNotConfigured))
	for _, name := range record.RulesNotConfigured {
		notConfigured[name] = true
	}

	silent := append([]string(nil), record.RulesSilent...)
	sort.Strings(silent)
	isSilent := make(map[string]bool, len(silent))
	summary := coverageSummary{Unnamed: map[coverageCategory]int{}}
	for _, name := range silent {
		if isSilent[name] {
			return coverageSummary{}, fmt.Errorf("the lint record names rule %s as silent twice", name)
		}
		isSilent[name] = true
		entry := ruleCoverageEntry{Name: name}
		switch {
		case notConfigured[name]:
			entry.Category = coverageNotConfigured
		case record.RulesScopedOff[name] > 0:
			entry.Category = coverageOffByTheConfiguration
			entry.Details = []string{fmt.Sprintf("%d files", record.RulesScopedOff[name])}
			// Why it is off, from the settings that said so, as a TypeScript run prints it.
			if reason := record.OffReasons[name]; reason != "" {
				entry.Details = append(entry.Details, "off because "+reason)
			} else if record.OffReasons != nil {
				entry.OffWithoutReason = true
			}
		default:
			entry.Category = coverageListenedToNothing
		}
		summary.Entries = append(summary.Entries, entry)
	}

	found := record.RulesRun - record.RulesWatchedAndQuiet - len(silent)
	if record.RulesWatchedAndQuiet < 0 || found < 0 {
		return coverageSummary{}, fmt.Errorf(
			"the lint record counts %d rules run, and %d watched and quiet plus %d silent is more than that",
			record.RulesRun,
			record.RulesWatchedAndQuiet,
			len(silent),
		)
	}
	summary.Unnamed[coverageRanAndFoundNothing] = record.RulesWatchedAndQuiet
	summary.Unnamed[coverageFoundSomething] = found

	// A rule the record places among the counted ones and also names as off, or unconfigured, for some
	// files ran on the rest. Its category is the counted one; the partial fact is said once, by name.
	for name, files := range record.RulesScopedOff {
		if !isSilent[name] {
			summary.PartialFacts = append(
				summary.PartialFacts,
				fmt.Sprintf("%s is off by the config for %d files and ran on the rest", name, files),
			)
		}
	}
	for name := range notConfigured {
		if !isSilent[name] {
			summary.PartialFacts = append(
				summary.PartialFacts,
				fmt.Sprintf("%s is not in the config for some files and ran on the rest", name),
			)
		}
	}
	sort.Strings(summary.PartialFacts)

	for _, crash := range record.Crashes {
		summary.Crashes = append(
			summary.Crashes,
			runCrash{Path: crash.File, Cause: crash.Error, Language: language.Name},
		)
	}
	summary.sortEntries()
	return summary, nil
}

// excludedNamedLimit is how many excluded files of one reason are named before they are counted, for the
// reasons nobody expects checked. A gap's files are always named.
const excludedNamedLimit = 5

// writeExcluded says which files the project record left out, once, under the lint line when
// there is one and above the phase line when there is not: a file nobody checked is a coverage fact
// whether or not lint got to run.
//
// Grouped by reason, and counted past a handful. The first run on ahraos-macos printed 107 lines of
// `vendored under Vendor/` between the lint line and the verdict, which buries the two lines a reader
// must act on under a hundred that say one thing. A few files are named, because one file left out
// for a surprising reason is the case worth seeing; a hundred for the same reason are one fact.
//
// A file under a directory this run yields is left out, whatever its reason: the run of the Swift project
// there accounts for it, so on ahraos-presence the root's notes stop naming CapturePhone's and RigSimulator's
// files as checked by nothing. What remains, for a reason other than the ones the run's language says nobody
// expects checked, is the run's gap: listed for the footer and `--json`, and named here file by file however
// many share a reason, since the limit is for one fact said a hundred times and each file of a gap is one to
// act on. A language that names no such reasons keeps no gap (see contractLanguage.ExpectedExclusionReasons).
//
// A file another build checked was checked, so its line leads with the reason that says by which build, in place
// of "not checked": `checked by the freebsd/arm64 build: …/memory_other.go`, in a Go run of every build (#72n9mq0).
func (run *contractRun) writeExcluded() {
	if run.excludedPrinted || run.project == nil {
		return
	}
	run.excludedPrinted = true

	filesByReason := map[string][]string{}
	reasons := []string{}
	add := func(file string, reason string) {
		if _, seen := filesByReason[reason]; !seen {
			reasons = append(reasons, reason)
		}
		filesByReason[reason] = append(filesByReason[reason], file)
	}
	var gap []uncheckedContractFile
	gapReasons := map[string]bool{}
	for _, excluded := range run.project.Excluded {
		// Resolved, since the child standardizes its paths as Foundation spells them, `/tmp` where this
		// process may hold `/private/tmp`, and the yielded directories are resolved too.
		if len(run.yieldedDirectories) > 0 && yieldsFile(run.yieldedDirectories, resolvedPath(excluded.File)) {
			continue
		}
		reason := excluded.Reason
		why, compiledUnlinted := run.notLinted[excluded.File]
		if compiledUnlinted && run.language.checkedElsewhere(reason) {
			// The project record said another build checks it before any build ran; the lint record says no rule
			// ran on it, so it is a file not checked, with why.
			reason = "not linted: " + why
		}
		add(excluded.File, reason)
		if run.language.ExpectedExclusionReasons != nil && !run.language.expectsExcluded(reason) {
			gap = append(gap, uncheckedContractFile{File: excluded.File, Reason: reason, Language: run.language.Name})
			gapReasons[reason] = true
		}
	}
	activeSummary.Gaps.Unchecked = gap
	run.countExcludedWhenNothingChecked()
	// A file the child could not read is printed the way an excluded file is, because to a reader it is
	// the same fact: a file nothing checked, and why.
	for _, unreadable := range run.unreadable {
		add(unreadable.File, "could not be read: "+unreadable.Error)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		files := filesByReason[reason]
		if run.language.checkedElsewhere(reason) {
			if len(files) > excludedNamedLimit {
				fmt.Fprintf(accountOutput(run.out), "  %s: %d files\n", reason, len(files))
				continue
			}
			for _, file := range files {
				fmt.Fprintf(accountOutput(run.out), "  %s: %s\n", reason, file)
			}
			continue
		}
		if len(files) > excludedNamedLimit && !gapReasons[reason] {
			fmt.Fprintf(accountOutput(run.out), "  not checked: %d files (%s)\n", len(files), reason)
			continue
		}
		for _, file := range files {
			fmt.Fprintf(accountOutput(run.out), "  not checked: %s (%s)\n", file, reason)
		}
	}
}

func (run *contractRun) acceptPhase(record *contractPhaseRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("a phase record in a %s run", run.mode)
	}
	if run.nextPhase >= len(phaseOrder) || string(phaseOrder[run.nextPhase]) != record.Name {
		expected := "nothing more"
		if run.nextPhase < len(phaseOrder) {
			expected = string(phaseOrder[run.nextPhase])
		}
		return fmt.Errorf("a phase record for %q where the pipeline order expects %s", record.Name, expected)
	}
	outcome, known := contractOutcomes[record.Outcome]
	if !known {
		return fmt.Errorf(
			"phase %s has outcome %q, which is not one of ran, skipped, notReached, reused",
			record.Name,
			record.Outcome,
		)
	}
	if phaseOrder[run.nextPhase] == phaseUnused {
		if len(run.unusedPending) > 0 {
			return fmt.Errorf(
				"%d unused records arrived with no unusedCoverage record to count them",
				len(run.unusedPending),
			)
		}
		if record.Outcome == "ran" && run.unusedCoverage == 0 {
			return fmt.Errorf("the unused phase ran and no unusedCoverage record said what it looked at")
		}
	}
	name := phaseOrder[run.nextPhase]
	if run.requested[name] && skippedAsNotRequested(phaseRecord{Outcome: outcome, Detail: record.Detail}) {
		return fmt.Errorf("the %s phase was asked for, and the child skipped it as %q", name, record.Detail)
	}
	run.nextPhase++
	run.findingsInPhase = 0
	run.report.record(name, outcome, time.Duration(record.ElapsedMilliseconds)*time.Millisecond, record.Detail)
	return nil
}

// contractRequestedPhases is which phases a command line asks for, read as the TypeScript run reads it: no
// phase flag asks for fix, types and lint; `--fix`, `--types` or `--lint` asks for only the ones named;
// and any `--unused` flag asks for unused as well. `--format-only` asks for fix alone, narrowed to
// formatting, so the child's types and lint skipped as not requested are no gap. Takes the flag reader as
// a parameter so a test can state its flags, as contractArguments does.
func contractRequestedPhases(given map[string]bool, value func(string) string) map[phaseName]bool {
	named := func(flag string) bool { return given[flag] && value(flag) == "true" }
	if named("format-only") {
		return map[phaseName]bool{phaseFix: true}
	}
	anyPhaseNamed := named("fix") || named("types") || named("lint")
	return map[phaseName]bool{
		phaseFix:    named("fix") || !anyPhaseNamed,
		phaseTypes:  named("types") || !anyPhaseNamed,
		phaseLint:   named("lint") || !anyPhaseNamed,
		phaseUnused: named("unused") || named("unused-all") || named("unused-deep"),
	}
}

func (run *contractRun) acceptRule(record *contractRuleRecord) error {
	if run.mode != contractModeRules && run.mode != contractModeRulesEnabled {
		return fmt.Errorf("a rule record in a %s run", run.mode)
	}
	run.rules = append(run.rules, *record)
	return nil
}

// acceptDuplicates prints the duplicates report, which comes once, after every phase, and only when it was asked for.
// It is never a finding: no count moves and the exit code stays the child's.
func (run *contractRun) acceptDuplicates(record *duplicate_functions.Report) error {
	if !run.duplicatesRequested {
		return fmt.Errorf("a duplicates record in a run that did not ask for --duplicates")
	}
	if run.duplicatesWritten {
		return fmt.Errorf("a second duplicates record")
	}
	if run.nextPhase < len(phaseOrder) {
		return fmt.Errorf("a duplicates record before the %s phase, and it comes after every phase",
			phaseOrder[run.nextPhase])
	}
	run.duplicatesWritten = true
	writeDuplicatesReport(run.out, *record)
	return nil
}

// acceptInputs keeps what a check run read, once. Every path must be absolute: the front door stats them from a
// working directory that need not be the child's, so a relative one would prove a different file unchanged. And a
// record naming no file is refused, since a check run reads at least the file its project is defined by, a go.mod or
// a Package.swift, and an empty proof matches any tree.
func (run *contractRun) acceptInputs(record *contractInputsRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("an inputs record in a %s run", run.mode)
	}
	if run.inputs != nil {
		return fmt.Errorf("a second inputs record")
	}
	if len(record.Files) == 0 {
		return fmt.Errorf("an inputs record naming no file, and a check run reads at least its project's definition")
	}
	for _, paths := range [][]string{record.Files, record.Directories, record.Absent} {
		for _, path := range paths {
			if !filepath.IsAbs(path) {
				return fmt.Errorf("an inputs record naming %q, which is not an absolute path", path)
			}
		}
	}
	run.inputs = record
	return nil
}

// acceptTiming prints what a Go run's CPU went to, once, under `--timing` (#3apgjer): as a table in the human views,
// under the lint line it arrives after, and written through as a `timing` line under `--json`. The record is the
// child's own type rather than a copy of it, since only cohere-go writes one.
//
// Refused in a run that did not ask, a second time, after the inputs record the contract places it before, with a
// list missing where the contract writes `[]`, and with a shared row of a kind this front door cannot label, each
// for the reason accept refuses an unknown record: a figure nobody can vouch for, or one printed under the wrong
// name, is worse than none.
func (run *contractRun) acceptTiming(record *contract.TimingRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("a timing record in a %s run", run.mode)
	}
	if !run.timingRequested {
		return fmt.Errorf("a timing record in a run that did not ask for one with --timing")
	}
	if run.timingReceived {
		return fmt.Errorf("a second timing record")
	}
	if run.inputs != nil {
		return fmt.Errorf("a timing record after the inputs record, and the contract places it before")
	}
	if record.Rules == nil || record.Shared == nil {
		return fmt.Errorf("a timing record missing its rules or shared list, which the contract writes [] when empty")
	}
	for _, row := range record.Shared {
		switch row.Kind {
		case contract.SharedIntermediateRepresentation, contract.SharedDispatcher, contract.SharedAnalyzer:
		default:
			return fmt.Errorf("a timing record's shared row %s of unknown kind %q", row.Name, row.Kind)
		}
	}
	run.timingReceived = true
	if activeOutput.Mode == outputJson {
		writeJsonLine(run.out, timingAsJson(*record))
	} else {
		printContractTimings(run.out, record)
	}
	streams.Show(run.out)
	return nil
}

// acceptTimeline prints when each phase and each build of a Go run began and ended, once, under `--timeline`
// (#3v2zp7g): as a table in the human views, after the lint line and the timing table when there is one, and written
// through as a `timeline` line under `--json`. The record is the child's own type, since only cohere-go writes
// one.
//
// Refused in a run that did not ask, a second time, before every phase has its record, and with its stamps missing
// where the contract writes `[]`, for the reason accept refuses an unknown record. That it is the last before the
// summary is accept's to hold, since only the record after it can break it.
func (run *contractRun) acceptTimeline(record *contract.TimelineRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("a timeline record in a %s run", run.mode)
	}
	if !run.timelineRequested {
		return fmt.Errorf("a timeline record in a run that did not ask for one with --timeline")
	}
	if run.timelineReceived {
		return fmt.Errorf("a second timeline record")
	}
	if run.nextPhase < len(phaseOrder) {
		return fmt.Errorf("a timeline record before the %s phase, and it comes after every phase",
			phaseOrder[run.nextPhase])
	}
	if record.Stamps == nil {
		return fmt.Errorf("a timeline record missing its stamps, which the contract writes [] when empty")
	}
	run.timelineReceived = true
	if activeOutput.Mode == outputJson {
		writeJsonLine(run.out, timelineAsJson(*record))
	} else {
		printGoTimeline(run.out, record)
	}
	streams.Show(run.out)
	return nil
}

// sayGoMemory prints, once, the limit a Go run's child held its heap under and why, with buildsAtOnce, how many
// builds beside the host's ran side by side under it, 0 for a run of one build or one whose lint never ran (#jrr6a5x).
// It is the invocation's, as the types line is, since a replay started no child. The lint record says the width, so
// the line waits for it, and the summary says it for a run that wrote none.
func (run *contractRun) sayGoMemory(buildsAtOnce int) {
	if run.goMemory == nil {
		return
	}
	printInvocation(accountOutput(run.out), "  %s\n", run.goMemory.line(buildsAtOnce))
	run.goMemory = nil
	streams.Show(run.out)
}

func (run *contractRun) acceptSummary(record *contractSummaryRecord) error {
	if run.mode != contractModeCheck {
		return fmt.Errorf("a summary record in a %s run", run.mode)
	}
	if record.Findings == nil || record.Complete == nil || record.ExitCode == nil {
		return fmt.Errorf("a summary missing findings, complete or exitCode")
	}
	if run.nextPhase != len(phaseOrder) {
		// The reason markRemainingNotReached exists: a phase absent from the report reads the same as
		// one the reporter forgot.
		return fmt.Errorf("a summary with no record for phase %s", phaseOrder[run.nextPhase])
	}
	// A run that asked for `--timing` and got no record would print nothing about its CPU and read as a run that
	// measured what was asked, so the stream is refused (#3apgjer): cohere-go writes the record on every check run
	// that asks.
	if run.timingRequested && !run.timingReceived {
		return fmt.Errorf("a summary with no timing record, in a run that asked for one with --timing")
	}
	// The same for `--timeline`, whose record cohere-go writes on every check run that asks (#3v2zp7g).
	if run.timelineRequested && !run.timelineReceived {
		return fmt.Errorf("a summary with no timeline record, in a run that asked for one with --timeline")
	}
	if run.duplicatesRequested && !run.duplicatesWritten {
		return fmt.Errorf("a summary with no duplicates record, which --duplicates asked for")
	}
	run.sayGoMemory(0)
	if *record.Findings != run.findings {
		return fmt.Errorf(
			"the summary counts %d findings and %d finding records arrived",
			*record.Findings,
			run.findings,
		)
	}

	// The two views of completeness are compared in one direction only. Where the records show a gap
	// and the summary claims none, the child is printing green over work it did not do, which is the
	// failure this tool exists to stop, and the run is refused. The other direction is allowed: a child
	// that says it fell short is believed, even where no record shows the gap.
	// A file not written was checked, and its fixes withheld, so it is named apart from the files nothing checked.
	uncheckedGaps := run.filesWithoutRecord > 0 || run.crashes > 0 || len(run.unreadable) > 0
	namedGaps := uncheckedGaps || len(run.notWritten) > 0
	// Judged by the phases the caller asked for: `complete` means every requested phase ran, so `--lint`
	// with fix and types skipped as not requested is complete. Any other skip is still a gap.
	gapInRecords := !run.report.checkedEveryRequestedPhase() || namedGaps
	if gapInRecords && *record.Complete {
		return fmt.Errorf("the summary calls the run complete, and its own records show what it did not check")
	}
	if missed := run.censusMissedTheLoad(); missed != "" && *record.Complete {
		return fmt.Errorf("the summary calls the run complete, and %s", missed)
	}
	// The files nothing checked are named on the line whatever else fell short: a phase that did not run is a gap of
	// its own, and with it alone the line read as if every file had reached the phases that did (#1hva1ya).
	if !*record.Complete && namedGaps {
		activeSummary.Gaps.Unread = contractUncheckedFiles(run.filesWithoutRecord, len(run.unreadable))
	}
	if !*record.Complete && run.report.checkedEveryRequestedPhase() {
		switch {
		case uncheckedGaps:
			run.report.incompleteBeyondPhases = "the notes above name the files nothing checked"
		case namedGaps:
			run.report.incompleteBeyondPhases = notWrittenSentence
		case run.censusMissedTheLoad() != "":
			run.report.incompleteBeyondPhases = run.censusMissedTheLoad()
			activeSummary.Gaps.CensusMissed = run.censusMissed
		default:
			run.report.incompleteBeyondPhases = run.language.programName() +
				" reported that it fell short without a record saying where"
			activeSummary.Gaps.Unread = run.language.programName() + " fell short without saying where"
		}
	}
	activeSummary.Gaps.NotWritten = run.notWritten
	// Whatever the run's completeness, which they never move: the child calls a run that withheld them complete.
	activeSummary.Gaps.WithheldInDependencyCgo = run.withheldInDependencyCgo
	activeSummary.Gaps.WithheldDependencyCgoDeclarations = run.withheldDependencyCgoDeclarations
	// Whatever the run's completeness too: every file was checked, and what fell short is a file go may have written.
	activeSummary.Gaps.UnguardedWorkspaceSums = record.UnguardedWorkspaceSums
	for _, sums := range record.UnguardedWorkspaceSums {
		printInvocation(accountOutput(run.out), "  go list may have rewritten %s\n", sums)
	}

	expectedExit := 0
	if run.findings > 0 || !*record.Complete {
		expectedExit = 1
	}
	if *record.ExitCode != expectedExit {
		return fmt.Errorf("the summary says exit %d, and %d findings in a run complete=%t means exit %d",
			*record.ExitCode, run.findings, *record.Complete, expectedExit)
	}

	run.summary = record
	run.writeExcluded()
	if run.project == nil {
		run.report.graphNotBuilt = true
	}
	if run.duplicatesUnanswered != "" {
		writeDuplicatesReport(run.out, duplicate_functions.Report{Floor: duplicate_functions.Floor,
			NotRun: run.duplicatesUnanswered})
	}
	writeRunEnd(run.report, run.out)
	return nil
}

// finish decides the run's exit code once the child has exited, from what it wrote and how it ended.
//
// Every path that is not a validated summary, or a listing mode's expected ending, is a failure that
// says nothing was checked. There is no path through here where a run that did not finish prints
// green.
func (run *contractRun) finish(childExit int, ended string) (int, error) {
	if childExit == 2 {
		// The child's own sentence is already on stderr, passed through untouched.
		return 1, fmt.Errorf("%s did not run, so nothing was checked", run.language.programName())
	}

	switch run.mode {
	case contractModeVersion:
		if childExit != 0 || run.provenance == nil {
			return 1, fmt.Errorf("%s %s without stating its version", run.language.programName(), ended)
		}
		fmt.Fprintln(run.out, versionReport())
		for _, line := range run.language.VersionLines(run.provenance, run.swiftSourceCommit) {
			fmt.Fprintln(run.out, line)
		}
		return 0, nil

	case contractModeRules, contractModeRulesEnabled:
		if childExit != 0 || run.provenance == nil {
			return 1, fmt.Errorf("%s %s before listing its rules", run.language.programName(), ended)
		}
		sort.Slice(run.rules, func(first, second int) bool { return run.rules[first].Name < run.rules[second].Name })
		for _, listed := range run.rules {
			if run.mode == contractModeRulesEnabled {
				fmt.Fprintf(run.out, "%s\t%s\n", listed.Name, listed.Severity)
			} else {
				fmt.Fprintln(run.out, listed.Name)
			}
		}
		return 0, nil
	}

	if run.summary == nil {
		run.writeUnfinished(fmt.Sprintf("%s %s before finishing", run.language.programName(), ended))
		return 1, fmt.Errorf("%s %s without a summary, so nothing was checked", run.language.programName(), ended)
	}
	if childExit != *run.summary.ExitCode {
		return 1, fmt.Errorf("%s %s, and its summary said it would exit %d",
			run.language.programName(), ended, *run.summary.ExitCode)
	}
	// The child calls a run whose every file was excluded clean, since it found nothing; the front door fails it,
	// since it checked nothing (#08cv2mp).
	if *run.summary.ExitCode == 0 && activeSummary.Gaps.nothingChecked() {
		return 1, nil
	}
	return *run.summary.ExitCode, nil
}

// censusMissedTheLoad is why a run is never complete when the child's types record names files its load reached that
// its census never counted (#yyv7nr2, #hd5bsqh): the census and the load read one tree, so no count of the run
// accounts for those files, and a run that checked none of them would read as one that checked everything. Empty
// when the record names none.
func (run *contractRun) censusMissedTheLoad() string {
	if len(run.censusMissed) == 0 {
		return ""
	}
	return fmt.Sprintf("its load reached %s in the %s that its census never counted, %s in %s first",
		counted(len(run.censusMissed), "file", "files"), run.language.Container, run.censusMissed[0].File,
		run.censusMissed[0].Package)
}

// countExcludedWhenNothingChecked fills the summary's reasons for a run that checked none of its files: each file
// the project record excluded, by its reason and the rule that decided it, and the files of ours a narrowing left
// out of scope. A file the run of a project below this one accounts for is that run's, as writeExcluded leaves it,
// and a file that was never a source to check, a Swift package's manifest, is no reason (NotSourceReasons).
// The package's files, filesInPackage, are the fact the run fails on, and a file of them no reason counts is named as
// unrecorded. A run that checked something keeps no reasons, and a project with no file of its language has none to
// keep, so both read as they did (#08cv2mp).
func (run *contractRun) countExcludedWhenNothingChecked() {
	if !activeSummary.Gaps.NothingToCheck || run.project == nil {
		return
	}
	for _, excluded := range run.project.Excluded {
		if len(run.yieldedDirectories) > 0 && yieldsFile(run.yieldedDirectories, resolvedPath(excluded.File)) {
			continue
		}
		if run.language.NotSourceReasons[excluded.Reason] {
			continue
		}
		activeSummary.Gaps.addExcluded(excluded.Reason, excluded.Source, 1)
	}
	if outOfScope := run.project.FilesOurs - run.project.FilesInScope; outOfScope > 0 {
		activeSummary.Gaps.addExcluded(outsideNamedPaths, "", outOfScope)
	}
	activeSummary.Gaps.SourceFiles = run.project.FilesInPackage
	activeSummary.Gaps.countUnrecorded()
}

// writeUnfinished prints the phase line for a run that stopped early, every phase the child never
// recorded marked not reached, so the last line a reader sees is never a clean bill of health.
func (run *contractRun) writeUnfinished(reason string) {
	if run.mode != contractModeCheck || run.summary != nil {
		return
	}
	for ; run.nextPhase < len(phaseOrder); run.nextPhase++ {
		run.report.record(phaseOrder[run.nextPhase], outcomeNotReached, 0, reason)
	}
	if run.project == nil {
		run.report.graphNotBuilt = true
	}
	writeRunEnd(run.report, run.out)
}

// truncateForError keeps a refused line short enough to read in an error.
func truncateForError(line []byte) string {
	const limit = 200
	if len(line) <= limit {
		return string(line)
	}
	return string(line[:limit]) + "…"
}

// contractUncheckedFiles says, for the footer, how many files nothing checked and why, since the notes naming
// them are --verbose's. A crashed file is counted apart, as the footer's own marker. Empty when the only
// gaps were crashes.
func contractUncheckedFiles(withoutRecord, unreadable int) string {
	total := withoutRecord + unreadable
	if total == 0 {
		return ""
	}
	var kinds []string
	if withoutRecord > 0 {
		kinds = append(kinds, fmt.Sprintf("%d with no compiler record", withoutRecord))
	}
	if unreadable > 0 {
		kinds = append(kinds, fmt.Sprintf("%d unreadable", unreadable))
	}
	return fmt.Sprintf("%s nothing checked: %s", counted(total, "file", "files"), strings.Join(kinds, ", "))
}
