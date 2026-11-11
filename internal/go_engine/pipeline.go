package go_engine

import (
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/format"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/settings"
	"github.com/system-inc/cohere/internal/go_engine/timing"
	"github.com/system-inc/cohere/internal/write_root"
)

// detailNotRequested is the one skip detail that means nobody asked for the phase, which is not a gap.
const detailNotRequested = "not requested"

// noFormatScope is the fix record's formatScope when `--no-format` left formatting out.
const noFormatScope = "formatting was left out, by --no-format"

// pipeline is one checking run, from the provenance record to the summary. It counts what it writes, so the
// summary's findings are the finding records written, and it remembers every gap, so the summary can never
// call complete a run whose own records show what it did not check.
type pipeline struct {
	request       invocation
	described     *describedProject
	settings      settings.Settings
	registered    []rule.Rule
	writer        *contract.Writer
	standardError io.Writer

	// memory is what Main decided about the process's memory limit, its record written on the types record, nil in a
	// test; loads is the limit across the run's loads, nil in a run that raises nothing (loadMemory).
	memory *engineMemory
	loads  *loadMemory

	// writeError is the first record that could not be written. Every write after it is skipped, and the run
	// ends as a broken stream.
	writeError error

	// findingsWritten counts the finding records written.
	findingsWritten int

	// incomplete is set by every gap: a requested phase not reached, an unreadable file, a file without a
	// compiler record, a file the formatter could not format, a rule that crashed.
	incomplete bool

	// session is the findings cache the load and the lint phase share, nil when the cache sat the run out, under
	// `--no-cache` unless the binary carries the standard library's entries, which a session without a store reads,
	// and always under `--timing`, whose every figure must be of work that ran (cacheSession).
	session *check.CacheSession

	// keepsHostRestoreOff, set only by a test, has every extra build check every package itself rather than restore any
	// from the host's run, so the test can compare the two (#b2t9xmf).
	keepsHostRestoreOff bool

	// buildsAtOnce is how many extra builds otherBuilds ran side by side, decided once as they started
	// (extraBuildsWidth), 0 until they did, for the lint record. buildsWidthForced, set only by a test, is the width
	// in place of that decision for builds that restore from the host, and buildsWidthUnguarded, a test's too, lets it
	// apply to builds that restore nothing, so a test can show the width reaches no key with builds that each check
	// every package. extraBuildEnds, also a test's alone, is called as each extra build's run ends with the build's own
	// notes, and fails the build when it returns an error, so a test can note from a build and fail one by name.
	buildsAtOnce         int
	buildsWidthForced    int
	buildsWidthUnguarded bool
	extraBuildEnds       func(extra project.Build, notes io.Writer) error

	// hostLoading and hostLinting are the host's build's wall time loading the workspace and running the rules over it,
	// in nanoseconds, over every load and pass, its build record's (buildRun). A load or a pass beside another runs on
	// a goroutine of its own, so each is added when it ends.
	hostLoading atomic.Int64
	hostLinting atomic.Int64

	// renamed is set once a rename has written, after which the lint phase runs without the findings cache, whose keys
	// are of the bytes the rename replaced. gofmt's verdicts, kept by their bytes, are still kept.
	renamed bool

	// fix is the fix phase's record, held unwritten from gofmt until the long-line wrap and the rename after the load
	// have added to it, nil when the phase was not requested.
	fix *heldFix

	// writeRoot is where the fix phase may write: the root the run was given, resolved once by runFix in a run that
	// writes, and the zero WriteRoot, which refuses every write, in one that does not (#hwz114g).
	writeRoot write_root.WriteRoot

	// withheld are the files gofmt, the wrap or a rename would have written and did not, since each is reached
	// through a link to a real path outside the root, by file with that real path. writeFix names them on the fix
	// record, and each is a gap.
	withheld map[string]string

	// refusedRenames are the renames refused so far, by file, old name and new name, so a rename a round refused and
	// whose finding still stands is not asked for, and counted, again by the next.
	refusedRenames map[[3]string]bool

	// buildRuns are the runs mergeBuildRuns was handed, the host's first, nil unless `--other-builds` found a build
	// beside the host's to check.
	buildRuns []buildRun

	// overlaps is whether a run whose findings cache has no store may start the lint phase's rules beside the load,
	// which only a test turns off, to hold a run that does to one that does not; speculation is that pass once started
	// (see pipeline_speculative_lint.go).
	overlaps    bool
	speculation *speculativeLint

	// timing collects what the run's CPU goes to under `--timing`, for the timing record, and is nil otherwise, which
	// every measurement reads as a run that did not ask (#3apgjer).
	timing *timing.Collector

	// duplicates is the duplicates report the lint phase built, nil until it has, and duplicatesNotRun why it never
	// did, under `--duplicates` (see pipeline_duplicates.go).
	duplicates       *duplicate_functions.Report
	duplicatesNotRun string

	// watchPrelude is a test's look at the run's prelude, nil outside a test: told as each load of the workspace
	// begins, as runFix starts and returns, and as the lint pass beside a load is abandoned, each on the goroutine that
	// does it (see pipeline_load_beside_fix.go).
	watchPrelude func(step preludeStep)
}

func newPipeline(
	request invocation,
	described *describedProject,
	ruleSettings settings.Settings,
	registered []rule.Rule,
	standardOutput io.Writer,
	standardError io.Writer,
	memory *engineMemory,
) *pipeline {
	return &pipeline{
		request:       request,
		described:     described,
		settings:      ruleSettings,
		registered:    registered,
		writer:        contract.NewWriter(standardOutput),
		standardError: standardError,
		memory:        memory,
		loads:         newLoadMemory(memory, request.switches["single-threaded"]),
		overlaps:      true,
		timing:        newTiming(request),
	}
}

// newTiming is a run's collector under `--timing`, and nil for a run that did not ask.
func newTiming(request invocation) *timing.Collector {
	if !request.switches["timing"] {
		return nil
	}
	return timing.New()
}

// timingKinds is the timing record's answer to which analyzer is which rule's, read from registered, the one place
// that knows: each registered rule's analyzer is its rule's; staticcheck's buildir and the engine's own intermediate
// representation build are the intermediate representation builds; a dispatcher's analyzer, found among what the
// rules require and known by its result as the checker knows it (dispatch.IsDispatcher), is a dispatcher's walk; and
// any other analyzer, inspect or ctrlflow, is one several rules read.
func timingKinds(registered []rule.Rule) timing.Kinds {
	rules := make(map[string]string, len(registered))
	dispatchers := map[string]bool{}
	visited := map[*analysis.Analyzer]bool{}
	var visit func(analyzer *analysis.Analyzer)
	visit = func(analyzer *analysis.Analyzer) {
		if visited[analyzer] {
			return
		}
		visited[analyzer] = true
		if dispatch.IsDispatcher(analyzer) {
			dispatchers[analyzer.Name] = true
		}
		for _, required := range analyzer.Requires {
			visit(required)
		}
	}
	for _, registeredRule := range registered {
		// A rule without an analyzer never runs (check.Run refuses it), so it has no row to name.
		if registeredRule.Analyzer == nil {
			continue
		}
		rules[registeredRule.Analyzer.Name] = registeredRule.Name
		visit(registeredRule.Analyzer)
	}
	builds := map[string]bool{
		build_intermediate_representation.Analyzer.Name: true,
	}
	return func(analyzer string) (string, contract.SharedKind) {
		if ruleName, isRule := rules[analyzer]; isRule {
			return ruleName, ""
		}
		if builds[analyzer] {
			return "", contract.SharedIntermediateRepresentation
		}
		if dispatchers[analyzer] {
			return "", contract.SharedDispatcher
		}
		return "", contract.SharedAnalyzer
	}
}

// run writes the whole stream, every phase once and in order, and returns the exit code the summary states.
func (engine *pipeline) run() int {
	engine.write(provenanceRecord())
	engine.write(engine.described.record)
	for _, unreadable := range engine.described.unreadable {
		engine.write(unreadable)
		engine.incomplete = true
	}

	needsTypes := engine.request.requests(contract.PhaseTypes) || engine.request.requests(contract.PhaseLint)
	// The long-line wrap waits for a load that finds the types sound, so a run that wraps loads even when no phase
	// reads the types.
	wraps := engine.wrapsLongLines()
	if needsTypes || wraps {
		engine.session = engine.cacheSession(engine.described.workspace.Environment(), cache.FileDigests{},
			engine.standardError)
		// The host's build keeps its packages' entries for the extra builds to restore from (#b2t9xmf).
		if engine.session != nil && engine.recordsReads() {
			engine.session.KeepForBuilds()
		}
	}
	var loaded *load.Loaded
	var loadError error
	if (needsTypes || wraps) && engine.loadsBesideFix() {
		// Under `--no-fix` the first load runs beside gofmt's check, which writes nothing the load reads.
		loaded, loadError = engine.loadBesideFix(wraps, needsTypes)
	} else {
		engine.runFix(wraps)
		// The load runs after gofmt, so it reads the formatted tree, and again only when the wrap wrote a file and a
		// phase reads the types.
		if needsTypes || wraps {
			loaded, loadError = engine.loadWorkspace(needsTypes)
		}
	}
	loaded, loadError = engine.wrapAfterLoad(loaded, loadError, needsTypes)
	// The rename reads the wrapped tree, and needs a sound load as the wrap does. When it writes, a load of what it
	// wrote is the one the types and lint phases read in place of this one.
	var linted *lintPass
	loaded, linted, loadError = engine.runRename(loaded, loadError)
	// The fix phase's records come before any of the types phase's, so the stream reads in phase order.
	engine.writeFix()
	if !needsTypes {
		// A load made for the wrap alone feeds no phase, and writes no inputs, as a run that asked for no types never
		// has.
		loaded, loadError = nil, nil
	}
	engine.runTypes(loaded, loadError)
	// The inputs are those of the load the lint ran over, the final round's when a reload ran it again, and the first
	// load is let go before a reload loads again.
	loaded = engine.runLint(loaded, loadError, linted)
	engine.phase(contract.PhaseUnused, contract.OutcomeSkipped, 0, detailNotRequested)
	engine.writeDuplicates()
	// The timing record, under `--timing`, once every phase that measures has run, so it holds all they spent.
	if engine.timing != nil {
		engine.write(engine.timing.Record(timingKinds(engine.registered)))
	}
	// The inputs come last before the summary, once every phase has read what it reads. A run whose load failed, or
	// that asked for none, writes no inputs, so the front door records nothing to replay for it: what a failed load
	// read is not known. The memory record comes just before them, under the same condition, once every load has
	// ended, so it names each raise that landed.
	if loaded != nil {
		engine.write(engine.raisesRecord())
		engine.write(engine.inputsRecord(loaded))
	}

	exitCode := exitClean
	if engine.findingsWritten > 0 || engine.incomplete {
		exitCode = exitFindings
	}
	engine.write(contract.SummaryRecord{
		Findings: engine.findingsWritten, Complete: !engine.incomplete, ExitCode: exitCode,
	})
	if engine.writeError != nil {
		return broken(engine.standardError, engine.writeError)
	}
	return exitCode
}

// write writes one record, counting findings, unless an earlier record already could not be written.
func (engine *pipeline) write(record contract.Record) {
	if engine.writeError != nil {
		return
	}
	if writeError := engine.writer.Write(record); writeError != nil {
		engine.writeError = writeError
		return
	}
	if _, isFinding := record.(contract.FindingRecord); isFinding {
		engine.findingsWritten++
	}
}

// phase writes a phase record. A phase not reached is always a gap, since only a requested phase can be.
func (engine *pipeline) phase(
	name contract.PhaseName, outcome contract.Outcome, elapsed time.Duration, detail string,
) {
	if outcome == contract.OutcomeNotReached {
		engine.incomplete = true
	}
	// The duplicates report reads the lint phase's pass, so a lint phase that did not run says why the report did not.
	if name == contract.PhaseLint && outcome != contract.OutcomeRan {
		engine.duplicatesNotRun = "the lint phase did not run (" + detail + "), and the report reads its pass"
	}
	engine.write(contract.PhaseRecord{
		Name: name, Outcome: outcome, ElapsedMilliseconds: int(elapsed.Milliseconds()), Detail: detail,
	})
}

// note writes a line of prose to stderr, which the front door passes through untouched.
func (engine *pipeline) note(template string, values ...any) {
	noteTo(engine.standardError, template, values...)
}

// noteTo writes a line of prose to notes as note writes it to stderr, for an extra build, which notes into a buffer
// of its own that otherBuilds writes to stderr in build order once every build has ended (extraBuildRuns).
func noteTo(notes io.Writer, template string, values ...any) {
	// Ignored: a note that cannot reach stderr changes no record, and the records carry the verdict.
	_, _ = fmt.Fprintf(notes, "cohere-go: "+template+"\n", values...)
}

// severityOf is the severity a rule's findings carry, and false when the settings turn the rule off.
func (engine *pipeline) severityOf(name string) (contract.Severity, bool) {
	switch engine.settings.Rules[name].Severity {
	case settings.SeverityOff:
		return "", false
	case settings.SeverityWarning:
		return contract.SeverityWarning, true
	}
	return contract.SeverityError, true
}

// heldFix is the fix phase's record while the run holds it unwritten (pipeline.fix), from gofmt until the long-line
// wrap and the rename after the load have added to it, with what the wrap needs.
type heldFix struct {
	record contract.FixRecord

	// elapsed is the fix phase's own time, gofmt's, the wrap's and the rename's, never the load's between them but the
	// loads only a rename reads.
	elapsed time.Duration

	// wraps is whether the long-line wrap runs after the load: a run that writes, formats and has the rule on.
	wraps bool

	// formatted are the readable files in scope that gofmt formatted or found formatted, the files the wrap may change:
	// never one gofmt could not format, nor a generated one.
	formatted []string

	// known is gofmt's verdicts, which the wrap reads and adds to before the run keeps them.
	known *format.KnownFiles

	// formatKnown and wrapKnown count the files gofmt and the wrap answered from known rather than formatting or
	// wrapping, each left as it was when the run last saw the same bytes. They are the engine's own account of what a
	// verdict spared, held beside the record rather than in it, since the fix record is the contract every engine
	// writes and none reports such a count yet (#3hygsbr).
	formatKnown int
	wrapKnown   int
}

// runFix is the fix and format phase's gofmt: over every readable file in scope, written unless `--no-fix`, and
// under `--no-fix` one finding per file gofmt would change, at the first line it would change. It holds the phase's
// record unwritten in engine.fix, so the long-line wrap after the load (wrapAfterLoad) and the renames the naming rules
// ask for (runRename) can add to it, and writeFix writes it before the types phase. No other analyzer fix is applied.
func (engine *pipeline) runFix(wraps bool) {
	engine.watch(preludeFixBegan)
	defer engine.watch(preludeFixReturned)
	if !engine.request.requests(contract.PhaseFix) {
		engine.phase(contract.PhaseFix, contract.OutcomeSkipped, 0, detailNotRequested)
		return
	}
	started := time.Now()
	candidates := engine.described.readable()
	held := &heldFix{record: contract.FixRecord{
		FilesConsidered:     len(candidates),
		RefusalsByReason:    map[string]int{},
		NotFormattedReasons: map[string]int{},
		FormatScope:         engine.described.scopeWords(),
		ChangedFiles:        []contract.ChangedFile{},
		NotWritten:          []contract.NotWrittenFile{},
	}}
	engine.fix = held
	// A run that writes resolves its root once, before any phase writes: gofmt, the wrap and the rename, which
	// `--no-format` still runs. A check writes nothing and asks nothing of it.
	if !engine.request.switches["no-fix"] {
		engine.writeRoot = engine.resolveWriteRoot()
	}
	if engine.request.switches["no-format"] {
		held.record.FormatScope = noFormatScope
		held.elapsed = time.Since(started)
		return
	}

	severity, reported := engine.severityOf(ruleRequireFormatting)
	held.known = engine.knownFiles()
	held.wraps = wraps
	// The editor's save formats in memory and writes nothing (runStdin).
	saves := engine.request.stdinPath != ""
	result := format.Run(candidates, format.Options{
		Write:          !engine.request.switches["no-fix"] && !saves,
		KeepOutput:     saves,
		Root:           engine.writeRoot,
		SingleThreaded: engine.request.switches["single-threaded"],
		Known:          held.known,
		Held:           engine.held,
	})
	held.formatKnown = result.Known
	// Neither a file gofmt could not format nor a generated one is the wrap's to change.
	skipped := map[string]bool{}
	for _, generated := range result.Generated {
		skipped[generated] = true
	}
	for _, notFormatted := range result.NotFormatted {
		skipped[notFormatted.File] = true
	}
	// Nor is a file gofmt's write was withheld from: it holds bytes gofmt would change, which the wrap must not read
	// as gofmt's output, and it is not this run's to write. Its finding is reported below, as a check reports it.
	for _, withheld := range result.Withheld {
		skipped[withheld.File] = true
		engine.withhold(withheld.File, withheld.RealPath)
	}
	for _, file := range candidates {
		if !skipped[file] {
			held.formatted = append(held.formatted, file)
		}
	}
	for _, changed := range result.Changed {
		// A save's file is rewritten in memory, as a run that writes rewrites it on disk.
		if changed.Written || changed.Output != nil {
			held.record.FilesRewritten++
			held.record.FilesReformatted++
			held.record.ChangedFiles = append(held.record.ChangedFiles,
				contract.ChangedFile{File: changed.File, FixedBy: map[string]int{}, Formatted: true})
			engine.hold(changed)
			continue
		}
		if reported {
			engine.write(contract.FindingRecord{
				Source:      contract.SourceFormat,
				File:        changed.File,
				Line:        changed.FirstDifferingLine,
				Column:      1,
				Severity:    severity,
				Rule:        ruleRequireFormatting,
				MessageId:   notFormattedMessageId,
				Message:     notFormattedMessage,
				Fixes:       []contract.Edit{},
				Suggestions: []contract.Suggestion{},
			})
		}
	}
	for _, notFormatted := range result.NotFormatted {
		// Keyed by kind ("it does not parse"), so one count reads as one fact; the file and the whole reason
		// go to stderr, where a reader can find which file it was.
		kind, _, _ := strings.Cut(notFormatted.Reason, ":")
		held.record.NotFormattedReasons[kind]++
		held.record.FilesNotFormatted++
		// A file nobody could format is one whose formatting nobody knows, so a run over it is not complete.
		engine.incomplete = true
		engine.note("%s was not formatted: %s", notFormatted.File, notFormatted.Reason)
	}
	held.elapsed = time.Since(started)
}

// writeFix writes the fix phase's record and its phase record, once the wrap and the rename after the load have added
// to them, and keeps gofmt's verdicts and the wrap's for the next run. Every file a write was withheld from is named on
// the record and on stderr, and makes the run incomplete: its fixes were not made, and a run that says nothing of
// them would read as one that fixed everything it could.
func (engine *pipeline) writeFix() {
	if engine.fix == nil {
		return
	}
	for _, file := range slices.Sorted(maps.Keys(engine.withheld)) {
		engine.fix.record.NotWritten = append(engine.fix.record.NotWritten,
			contract.NotWrittenFile{File: file, RealPath: engine.withheld[file]})
		engine.incomplete = true
		engine.note("%s was not written: it is reached through a link to %s, outside the root, so its fixes are "+
			"withheld and its findings stand", file, engine.withheld[file])
	}
	engine.keepKnownFiles(engine.fix.known)
	engine.write(engine.fix.record)
	engine.phase(contract.PhaseFix, contract.OutcomeRan, engine.fix.elapsed, "")
}

// resolveWriteRoot is the WriteRoot of the root the run was given. A root that cannot be resolved, which the run has
// already read, leaves the zero WriteRoot, so every write is withheld and named rather than made blind.
func (engine *pipeline) resolveWriteRoot() write_root.WriteRoot {
	writeRoot, rootError := write_root.New(engine.described.workspace.Root)
	if rootError != nil {
		engine.note("the root could not be resolved, so no fix is written: %v", rootError)
	}
	return writeRoot
}

// withhold keeps file, a write refused since its real path is outside the root, for writeFix to name.
func (engine *pipeline) withhold(file string, realPath string) {
	if engine.withheld == nil {
		engine.withheld = map[string]string{}
	}
	engine.withheld[file] = realPath
}

// wrapsLongLines is whether the fix phase wraps the long-line rule's findings: only in a run that writes, formats and
// asks for fixes, so neither `--no-fix`, `--no-format` nor `--format-only` gets one, and only while the registry holds
// the rule and the settings leave it on, so the fix never acts for a rule that reports nothing.
func (engine *pipeline) wrapsLongLines() bool {
	if !engine.request.requests(contract.PhaseFix) || engine.request.switches["no-fix"] ||
		engine.request.switches["no-format"] || engine.request.switches["format-only"] {
		return false
	}
	if _, on := engine.severityOf(consistency_no_long_line.Name); !on {
		return false
	}
	return slices.ContainsFunc(engine.registered, func(registered rule.Rule) bool {
		return registered.Name == consistency_no_long_line.Name
	})
}

// knownFilesLabel names gofmt's verdicts among what the findings cache keeps.
const knownFilesLabel = "gofmt verdicts"

// knownFiles is gofmt's verdicts the findings cache kept from an earlier run, nil without the cache or its store, so
// a run under `--no-cache` neither reads nor keeps them.
func (engine *pipeline) knownFiles() *format.KnownFiles {
	if engine.session == nil || !engine.session.HasStore() {
		return nil
	}
	encoded, _ := engine.session.Kept(engine.session.KeyFor(knownFilesLabel))
	return format.DecodeKnownFiles(encoded)
}

// keepKnownFiles keeps gofmt's verdicts for the next run.
func (engine *pipeline) keepKnownFiles(known *format.KnownFiles) {
	if known == nil {
		return
	}
	encoded, encodeError := known.Encode()
	if encodeError == nil {
		encodeError = engine.session.Keep(engine.session.KeyFor(knownFilesLabel), encoded)
	}
	if encodeError != nil {
		engine.note("the findings cache could not keep gofmt's verdicts, so the next run formats every file: %v",
			encodeError)
	}
}

// hold makes the bytes the run holds of a file the fix phase changed the bytes the change left: the output itself for
// a save, which formats in memory (format.Options.KeepOutput), and the disk's, read again, for a run that wrote it.
func (engine *pipeline) hold(changed format.ChangedFile) {
	if changed.Output != nil {
		engine.described.contents[changed.File] = changed.Output
		return
	}
	engine.reread(changed.File)
}

// reread reads a file the fix phase just rewrote, so the suppression comments lint reads are on the lines the
// load and the rules see: gofmt can join blank lines, which moves every line below.
//
// A process that refuses writes, the editor's save (format.RefuseWrites), rewrote nothing, and the file it names holds
// what the editor has not saved, so it reads nothing again: the disk's bytes would take the place of the buffer. A
// write it was asked for anyway was refused, and the bytes it holds are the ones that stand.
func (engine *pipeline) reread(file string) {
	if format.WritesRefused() {
		return
	}
	contents, readError := os.ReadFile(file)
	if readError != nil {
		// Without its contents the file gets no suppression index, so its findings are kept rather than
		// withheld by directives on lines that may have moved.
		delete(engine.described.contents, file)
		engine.note("%s was rewritten and could not be read again, so its //cohere:ignore comments are not "+
			"read: %v", file, readError)
		return
	}
	engine.described.contents[file] = contents
}

// held is the bytes the run holds for file, as load.Held asks: described.contents, every readable file in scope as it
// is on disk, since describeProject read it and each write after was reread (reread). Every reader that would open a
// file again reads it here instead: gofmt's check, the load, and a rule through sourceReader. The one time the map
// runs ahead of the disk is inside the rename, whose askedOver sets a file to the rewrite before writeRenamed writes
// and rereads it, and there the rename's loads read the disk and its rules read the rewrite from the overlay first. The
// editor's save is the other: its one file is the buffer, and gofmt's and the wrap's output of it, never the disk's.
// described.contents is written only on the main goroutine, between phases, and the goroutines of gofmt, the load and
// the rules read it only while that goroutine waits for them, so it is never read while it is written and takes no
// lock. The rules beside the load (speculativeLint) read it while the main goroutine runs the long-line wrap, which
// stops them before its first reread, and under `--no-fix` gofmt's check reads it on a goroutine of its own beside the
// first load and those rules, and writes none of it (loadsBesideFix).
func (engine *pipeline) held(file string) ([]byte, bool) {
	contents, isHeld := engine.described.contents[file]
	return contents, isHeld
}

// sourceReader is what a rule's pass.ReadFile reads through over a load that parsed overlay's bytes in place of the
// disk's and rewritten's in place of a cgo output's: a file's bytes in overlay, else in rewritten, else the bytes the
// run holds for it, else the disk, so a rule reads the bytes the load parsed without reading a file it holds again.
// The run holds each readable file in scope as every load after the fix phase's writes parsed it, since each write is
// reread before the next load (reread), and the rename's proof overlays its rewrite, which askedOver gives here. A
// file it holds nothing for, a dependency's, a cgo output the load did not rewrite, one outside the scope or one whose
// reread failed, is read from disk, where the load read it. The bytes are shared rather than copied, which holds while
// every reader only reads them, as every analyzer does.
func (engine *pipeline) sourceReader(
	overlay map[string][]byte,
	rewritten *load.Rewritten,
) func(file string) ([]byte, error) {
	return func(file string) ([]byte, error) {
		if contents, overlaid := overlay[file]; overlaid {
			return contents, nil
		}
		// A cgo output the load rewrote differs from the build cache's file in its `//line` directives' paths, so
		// the offsets its syntax holds are into the rewritten bytes alone.
		if contents, isRewritten := rewritten.Lookup(file); isRewritten {
			return contents, nil
		}
		if contents, isHeld := engine.held(file); isHeld {
			return contents, nil
		}
		return os.ReadFile(file)
	}
}

// runTypes is the types phase: every list, parse and type error the load met is a compiler finding, but those it
// withheld in a dependency the cgo tool would translate, which the record names instead, as it counts the
// dependency's declarations the rules were given without what reaches cgo, and every file in scope that no
// analyzed package holds is a file nothing saw.
func (engine *pipeline) runTypes(loaded *load.Loaded, loadError error) {
	if !engine.request.requests(contract.PhaseTypes) {
		engine.phase(contract.PhaseTypes, contract.OutcomeSkipped, 0, detailNotRequested)
		return
	}
	if loadError != nil {
		engine.phase(contract.PhaseTypes, contract.OutcomeNotReached, 0, "the load failed: "+oneLine(loadError))
		return
	}
	for _, diagnostic := range loaded.Diagnostics {
		engine.write(engine.compilerFinding(diagnostic))
	}
	withoutRecord := engine.filesWithoutRecord(loaded)
	if len(withoutRecord) > 0 {
		engine.incomplete = true
	}
	withheld := make([]contract.WithheldFile, 0, len(loaded.WithheldInDependencyCgo))
	for _, each := range loaded.WithheldInDependencyCgo {
		if each.Errors < 1 {
			// Invariant: load's withheldErrors makes an entry only by counting an error it withheld, so a file with
			// none is a bug in the load, and written it would read as a file withheld for nothing. A panic ends the
			// stream before its summary, which the front door reports as an engine that checked nothing.
			panic(fmt.Sprintf("go_engine: %s is withheld with %d type errors", each.File, each.Errors))
		}
		withheld = append(withheld, contract.WithheldFile{Package: each.Package, File: each.File, Errors: each.Errors})
	}
	// The build text says what ran: go list, and for a project with cgo of its own go list -compiled, whose cgo tool
	// translated the packages with cgo files. It reads the same with the findings cache or without, so a run under
	// `--no-cache` writes the same record.
	build := "go list -deps with go test's variants built here, each package type-checked from source unless the " +
		"findings cache holds its export data"
	if loaded.CgoTranslated {
		build = "go list -deps with go test's variants built here, cgo translated by go list -compiled for the " +
			"packages with cgo files, each package type-checked from source unless the findings cache holds its " +
			"export data"
	}
	engine.write(contract.TypesRecord{
		Diagnostics:         len(loaded.Diagnostics),
		Files:               engine.filesTypeChecked(loaded),
		ElapsedMilliseconds: int(loaded.Elapsed.Milliseconds()),
		FilesWithoutRecord:  withoutRecord,
		Build: fmt.Sprintf("%s: %d packages, %d in the workspace", build,
			loaded.PackagesLoaded, loaded.PackagesInWorkspace),
		WithheldInDependencyCgo:           withheld,
		WithheldDependencyCgoDeclarations: loaded.WithheldDependencyCgoDeclarations,
		CgoTranslation: contract.CgoTranslation{
			Packages:     loaded.CgoTranslatedPackages,
			Milliseconds: int(loaded.CgoTranslationElapsed.Milliseconds()),
		},
		Memory: engine.memoryRecord(),
	})
	engine.phase(contract.PhaseTypes, contract.OutcomeRan, loaded.Elapsed, "")
}

// filesTypeChecked counts the distinct files in scope that the analyzed packages hold. A file the project record names
// as excluded is type-checked with its package all the same, and never counted, so a run with none in scope reads 0
// files. It reads GoFiles, the files as written, since a translated cgo package's CompiledGoFiles are generated files
// in the build cache.
func (engine *pipeline) filesTypeChecked(loaded *load.Loaded) int {
	seen := map[string]bool{}
	for _, analyzedPackage := range loaded.Packages {
		for _, file := range analyzedPackage.GoFiles {
			if engine.described.inScopeSet[file] {
				seen[file] = true
			}
		}
	}
	return len(seen)
}

// compilerFinding is one load diagnostic as a finding. go/types has no stable public code for its errors, so
// the rule is empty and the messageId is the diagnostic's kind. A diagnostic that names no file is placed at
// the root's go.work or go.mod, and a missing line or column at 1, because the front door refuses a finding
// with no position and an error is never dropped for lack of one.
func (engine *pipeline) compilerFinding(diagnostic load.Diagnostic) contract.FindingRecord {
	file := diagnostic.File
	if file == "" {
		file = engine.described.manifest
	}
	return contract.FindingRecord{
		Source:      contract.SourceCompiler,
		File:        file,
		Line:        max(diagnostic.Line, 1),
		Column:      max(diagnostic.Column, 1),
		Severity:    contract.SeverityError,
		Rule:        "",
		MessageId:   string(diagnostic.Kind),
		Message:     diagnostic.Message,
		Fixes:       []contract.Edit{},
		Suggestions: []contract.Suggestion{},
	}
}

// filesWithoutRecord are the files in scope that the project record called ours and nothing type-checked, each
// once and sorted, from three sources that each catch what the others may not: the load's Unseen, which no package
// holds though the constraints would build them; any file the load excluded that the project record did not name,
// since the two judging a file differently must show as a gap; and any readable file in scope that no analyzed
// package holds, the engine's own check, which needs nothing from the load but its packages.
func (engine *pipeline) filesWithoutRecord(loaded *load.Loaded) []string {
	named := map[string]bool{}
	for _, excluded := range engine.described.record.Excluded {
		named[excluded.File] = true
	}
	held := map[string]bool{}
	for _, analyzedPackage := range loaded.Packages {
		for _, file := range analyzedPackage.GoFiles {
			held[file] = true
		}
	}
	candidates := slices.Clone(loaded.Unseen)
	for _, excluded := range loaded.Excluded {
		if !named[excluded.File] {
			candidates = append(candidates, excluded.File)
		}
	}
	for _, file := range engine.described.readable() {
		if !held[file] {
			candidates = append(candidates, file)
		}
	}
	withoutRecord := []string{}
	for _, file := range candidates {
		if engine.described.inScopeSet[file] {
			withoutRecord = append(withoutRecord, file)
		}
	}
	slices.Sort(withoutRecord)
	return slices.Compact(withoutRecord)
}

// oneLine is an error on one line, for a phase record's detail, which prints inside one line.
func oneLine(cause error) string {
	return strings.Join(strings.Fields(cause.Error()), " ")
}
