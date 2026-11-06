package go_engine

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/format"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/settings"
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

	// writeError is the first record that could not be written. Every write after it is skipped, and the run
	// ends as a broken stream.
	writeError error

	// findingsWritten counts the finding records written.
	findingsWritten int

	// incomplete is set by every gap: a requested phase not reached, an unreadable file, a file without a
	// compiler record, a file the formatter could not format, a rule that crashed.
	incomplete bool

	// session is the findings cache the load and the lint phase share, nil under `--no-cache` or when the cache
	// sat the run out.
	session *check.CacheSession
}

func newPipeline(
	request invocation,
	described *describedProject,
	ruleSettings settings.Settings,
	registered []rule.Rule,
	standardOutput io.Writer,
	standardError io.Writer,
) *pipeline {
	return &pipeline{
		request:       request,
		described:     described,
		settings:      ruleSettings,
		registered:    registered,
		writer:        contract.NewWriter(standardOutput),
		standardError: standardError,
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
	if needsTypes {
		engine.session = engine.cacheSession()
	}
	engine.runFix()
	var loaded *load.Loaded
	var loadError error
	// The load runs once, after the fix phase so it reads the formatted tree, whenever a phase needs types.
	if needsTypes {
		workspace := engine.described.workspace
		if engine.session != nil {
			loaded, loadError = load.PackagesCached(workspace.Root, workspace.Patterns(), workspace.Environment(),
				engine.described.readableOurs, load.Caching{Restorer: engine.session, Listings: engine.session})
		} else {
			loaded, loadError = load.Packages(workspace.Root, workspace.Patterns(), workspace.Environment(),
				engine.described.readableOurs)
		}
		if loadError != nil {
			engine.note("the load failed, so nothing was type-checked or linted: %v", loadError)
		}
	}
	engine.runTypes(loaded, loadError)
	engine.runLint(loaded, loadError)
	engine.phase(contract.PhaseUnused, contract.OutcomeSkipped, 0, detailNotRequested)
	// The inputs come last before the summary, once every phase has read what it reads. A run whose load failed, or
	// that asked for none, writes no inputs, so the front door records nothing to replay for it: what a failed load
	// read is not known.
	if loaded != nil {
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
	engine.write(contract.PhaseRecord{
		Name: name, Outcome: outcome, ElapsedMilliseconds: int(elapsed.Milliseconds()), Detail: detail,
	})
}

// note writes a line of prose to stderr, which the front door passes through untouched.
func (engine *pipeline) note(template string, values ...any) {
	// Ignored: a note that cannot reach stderr changes no record, and the records carry the verdict.
	_, _ = fmt.Fprintf(engine.standardError, "cohere-go: "+template+"\n", values...)
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

// runFix is the fix and format phase: gofmt over every readable file in scope, written unless `--no-fix`, and
// under `--no-fix` one finding per file gofmt would change, at the first line it would change. Analyzer fixes
// are not applied yet, since applying them means loading again after the write.
func (engine *pipeline) runFix() {
	if !engine.request.requests(contract.PhaseFix) {
		engine.phase(contract.PhaseFix, contract.OutcomeSkipped, 0, detailNotRequested)
		return
	}
	started := time.Now()
	candidates := engine.described.readable()
	record := contract.FixRecord{
		FilesConsidered:     len(candidates),
		RefusalsByReason:    map[string]int{},
		NotFormattedReasons: map[string]int{},
		FormatScope:         engine.described.scopeWords(),
		ChangedFiles:        []contract.ChangedFile{},
	}
	if engine.request.switches["no-format"] {
		record.FormatScope = noFormatScope
		engine.write(record)
		engine.phase(contract.PhaseFix, contract.OutcomeRan, time.Since(started), "")
		return
	}

	severity, reported := engine.severityOf(ruleRequireFormatting)
	known := engine.knownFiles()
	result := format.Run(candidates, format.Options{
		Write:          !engine.request.switches["no-fix"],
		SingleThreaded: engine.request.switches["single-threaded"],
		Known:          known,
	})
	engine.keepKnownFiles(known)
	for _, changed := range result.Changed {
		if changed.Written {
			record.FilesRewritten++
			record.FilesReformatted++
			record.ChangedFiles = append(record.ChangedFiles,
				contract.ChangedFile{File: changed.File, FixedBy: map[string]int{}, Formatted: true})
			engine.reread(changed.File)
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
		record.NotFormattedReasons[kind]++
		record.FilesNotFormatted++
		// A file nobody could format is one whose formatting nobody knows, so a run over it is not complete.
		engine.incomplete = true
		engine.note("%s was not formatted: %s", notFormatted.File, notFormatted.Reason)
	}
	engine.write(record)
	engine.phase(contract.PhaseFix, contract.OutcomeRan, time.Since(started), "")
}

// knownFilesLabel names gofmt's verdicts among what the findings cache keeps.
const knownFilesLabel = "gofmt verdicts"

// knownFiles is gofmt's verdicts the findings cache kept from an earlier run, nil without the cache.
func (engine *pipeline) knownFiles() *format.KnownFiles {
	if engine.session == nil {
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

// reread reads a file the fix phase just rewrote, so the suppression comments lint reads are on the lines the
// load and the rules see: gofmt can join blank lines, which moves every line below.
func (engine *pipeline) reread(file string) {
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
	// The build text says which load ran: from source, or through go/packages for a project with cgo of its own. It
	// reads the same with the findings cache or without, so a run under `--no-cache` writes the same record.
	build := "go list -deps with go test's variants built here, each package type-checked from source unless the " +
		"findings cache holds its export data"
	if loaded.CgoTranslated {
		build = "go/packages with go list -deps -test -compiled, the project's cgo translated by the cgo tool, " +
			"each package type-checked from source, the findings cache restoring analysis only"
	}
	engine.write(contract.TypesRecord{
		Diagnostics:         len(loaded.Diagnostics),
		Files:               loaded.FilesTypeChecked,
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
	})
	engine.phase(contract.PhaseTypes, contract.OutcomeRan, loaded.Elapsed, "")
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
