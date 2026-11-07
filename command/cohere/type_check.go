package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/sentence"
	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/internal/types/program"
)

// collectTypeDiagnostics gathers the compiler's own findings for our files.
//
// The program is checked as a whole and then filtered down, rather than asked file by file. That
// ordering is load-bearing for speed and not at all for correctness: see Graph.AllDiagnostics. It
// does mean the compiler also checks the 6,500 third-party declarations, which is unavoidable (they
// have to be checked for our files to mean anything), and their findings are dropped here because
// nobody working in this repo can act on them.
//
// When the tsconfig is incremental, the check runs warm: files whose contents have not changed
// since the last run are not re-checked. Measured on the ahra tree, 1149ms cold against 380ms
// warm, with the finding set identical in both directions. A tsconfig without `incremental`
// falls back to the full pass, which is also what happens on the first run, after a toolchain
// version change, and any time the build info cannot be read.
//
// The build info is written even when this run reports findings, and that is deliberate: it
// records which files were checked, not whether they passed. A file that type-checked and
// produced a diagnostic was still checked. Discarding that on failure would make the slow path
// the one a person hits while iterating on an error, which is exactly the loop where the pause
// costs most.
//
// incremental is false under `--no-cache`, and then the build info is neither read nor written: the
// whole program is checked, as it is for a tsconfig that is not incremental.
//
// persist is false under `--no-fix`. The build info is still read, so the run is as warm as the last
// writing run left it, and only the write is withheld. A `--no-fix` run is therefore warm against a
// slightly older snapshot rather than cold, which costs it the files changed since then and nothing
// else; reading costs nothing the promise forbids, and refusing to read would make every CI run cold
// for no reason.
func collectTypeDiagnostics(
	callerContext context.Context,
	graph *program.Graph,
	files []*ast.SourceFile,
	incremental bool,
	persist bool,
) []*ast.Diagnostic {
	return finishTypeCheck(callerContext, startTypeCheck(callerContext, graph, incremental), files, persist)
}

// typeCheck is TypeScript's own checking of a graph, under way or done.
type typeCheck struct {
	graph   *program.Graph
	session *program.IncrementalSession
	checked []*ast.Diagnostic
	done    chan struct{}

	// reuse is the types section of the run cache's table, nil when this run is not recorded. replayed is
	// how many files' semantic diagnostics the check took from it rather than checking.
	reuse    *program.TypeDiagnosticsReuse
	replayed int

	// elapsed is how long the checking itself took, and finished when it ended, both set as it finishes. The
	// phase reports these rather than its wait, which is nothing when the check ran alongside the walk.
	elapsed  time.Duration
	finished time.Time
}

// earlyTypeCheckReleased is closed once the fix phase and any rebuild are over, for COHERE_TEST_HOLD_EARLY_CHECK.
var (
	earlyTypeCheckReleased    = make(chan struct{})
	releaseEarlyTypeCheckOnce sync.Once
)

// startTypeCheck begins checking every file, and returns at once.
//
// It runs alongside the fix phase's walk rather than after it. Both use the same checkers, each file under
// its own checker's lock, so on one checker the two take turns while different checkers go on in parallel:
// the run ends when the slowest checker has done its walk and its check, rather than after the slowest
// walk and then the whole check. Traced on ahra (#zqsdzbq), the check was a separate 0.1 to 0.2s after a
// walk whose slowest checker set its end.
//
// Only the per-file checking starts early. What reads every checker at once, the global and config
// diagnostics, waits for finishTypeCheck, when the walk has let go of them.
//
// Nothing ever cancels it. The compiler's whole-program check is not cancellable part way: a checker that
// sees its context end marks itself cancelled and the compiler hands it the next file anyway, where it
// panics on a goroutine nothing here can recover. A check whose graph is replaced is retired instead (see
// program.Graph.Retire), left to finish, and its result dropped.
func startTypeCheck(callerContext context.Context, graph *program.Graph, incremental bool) *typeCheck {
	checkContext := callerContext
	check := &typeCheck{graph: graph, done: make(chan struct{}), reuse: activeTypesReuse()}
	// One session across check-then-write. The build info has to be emitted from the same
	// incremental program that did the checking: that program's snapshot is what records which
	// files were checked, and emitting from a second one writes a build info that skips nothing
	// while looking correct. See program.IncrementalSession.
	go func() {
		// A test instrument: hold the check until the fix phase and any rebuild are over, so a test can put
		// the rebuild ahead of the check every time rather than by luck. See releaseEarlyTypeCheck.
		if os.Getenv("COHERE_TEST_HOLD_EARLY_CHECK") != "" {
			<-earlyTypeCheckReleased
		}
		start := time.Now()
		defer close(check.done)
		defer func() { check.elapsed, check.finished = time.Since(start), time.Now() }()
		// Begun here rather than before, since reading the build info and snapshotting the program is work
		// too, and it overlaps the walk as well as the check does.
		// Files whose shape fingerprint has not moved replay their semantic diagnostics, and only the rest are
		// checked. See program.TypeDiagnosticsReuse.
		if check.reuse != nil {
			if parts, reused := graph.CheckReusing(checkContext, check.reuse); reused {
				check.checked, check.replayed = parts.All(), check.reuse.Replayed()
				return
			}
		}
		if incremental {
			check.session = graph.NewIncrementalSession()
		}
		var parts program.TypeDiagnosticParts
		switch {
		case graph.FusedCheck != nil && check.session == nil:
			// The walk checked the files it visited, and this checks the rest.
			parts = graph.FusedCheck.DiagnosticParts(checkContext, graph)
		case check.session != nil:
			parts = check.session.DiagnosticParts(checkContext)
		default:
			parts = graph.AllDiagnosticParts(checkContext)
		}
		check.checked = parts.All()
		if check.reuse != nil {
			graph.RecordFull(checkContext, check.reuse, parts)
		}
	}()
	return check
}

// releaseEarlyTypeCheck lets a held check start. Called when the fix phase is over, whether or not anything
// was held.
func releaseEarlyTypeCheck() {
	releaseEarlyTypeCheckOnce.Do(func() { close(earlyTypeCheckReleased) })
}

// finishTypeCheck waits for the check, writes the build info if asked, and returns the diagnostics that
// belong to files, plus the program's own.
func finishTypeCheck(
	callerContext context.Context,
	check *typeCheck,
	files []*ast.SourceFile,
	persist bool,
) []*ast.Diagnostic {
	<-check.done

	// The read already happened when the session began; only the write is conditional.
	if check.session != nil && persist {
		if writeDiagnostics := check.session.Write(callerContext); len(writeDiagnostics) > 0 {
			// A failed build-info write must not pass silently. The next run would be cold while
			// this one reported success, and the symptom is a saving that quietly never appears.
			for _, diagnostic := range writeDiagnostics {
				fmt.Fprintf(streams.Errors, "cohere: %s\n",
					sentence.Of(fmt.Sprintf("writing the incremental cache: %s", diagnostic.MessageKey())))
			}
			showWarning()
		}
	}

	ours := make(map[*ast.SourceFile]struct{}, len(files))
	for _, sourceFile := range files {
		ours[sourceFile] = struct{}{}
	}
	diagnostics := check.graph.ConfigDiagnostics(callerContext)
	if check.reuse != nil {
		check.reuse.FinishGlobals(len(check.graph.Program.GetGlobalDiagnostics(callerContext)) == 0)
	}
	for _, diagnostic := range check.checked {
		// A diagnostic with no file is about the program rather than about any one file, so it is ours
		// by default: dropping it would hide exactly the configuration errors that matter most.
		if sourceFile := diagnostic.File(); sourceFile != nil {
			if _, isOurs := ours[sourceFile]; !isOurs {
				continue
			}
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	return diagnostics
}

// finishScopedTypeCheck finishes the check over the files the settings leave type-checked, and returns them with
// their diagnostics (#cxeq6jw). The settings say which files a plain ignore keeps out of every check, so a run
// that neither fixes nor lints, lintConfiguration nil, loads them here: `--types` reported the type errors of
// files the settings never check. Until lint counts its own, the summary counts here as in a run lint reaches:
// a file a plain ignore matched is not checked, and one an entry keeping the type check matched is types-only.
func finishScopedTypeCheck(
	callerContext context.Context,
	check *typeCheck,
	graph *program.Graph,
	location projectLocation,
	lintConfiguration *configuration.Configuration,
	projectFiles []*ast.SourceFile,
	persist bool,
) ([]*ast.SourceFile, []*ast.Diagnostic, error) {
	if lintConfiguration == nil {
		loaded, loadError := loadLintConfiguration(graph, location)
		if loadError != nil {
			return nil, nil, fmt.Errorf("loading the lint config: %w", loadError)
		}
		lintConfiguration = loaded
	}
	typeChecked, typesOnly := typeCheckedFiles(projectFiles, lintConfiguration)
	activeSummary.FilesInScope = len(typeChecked) - typesOnly
	activeSummary.FilesChecked, activeSummary.FilesTypesOnly = activeSummary.FilesInScope, typesOnly
	return typeChecked, finishTypeCheck(callerContext, check, typeChecked, persist), nil
}

// typeCheckedFiles is files less those a plain ignore matched, the files the type check reports on, and how many
// of those kept an entry naming `"check": "types"` matched (#cxeq6jw). A plain ignore means never checked at all,
// so the files it matched are left out here and finishTypeCheck drops their type errors as it drops a
// dependency's: the whole program is still checked, since our files' types depend on theirs, and nothing found in
// them is said. An entry keeping the type check keeps them, never linted or formatted, and counted apart.
// ignores is the run's settings, nil when there are none, which ignores nothing.
func typeCheckedFiles(files []*ast.SourceFile, ignores *configuration.Configuration) ([]*ast.SourceFile, int) {
	if ignores == nil {
		return files, 0
	}
	checked := make([]*ast.SourceFile, 0, len(files))
	typesOnly := 0
	for _, sourceFile := range files {
		resolved := ignores.Resolve(sourceFile.FileName().AsString())
		if resolved.Ignored && !resolved.TypesOnly {
			continue
		}
		if resolved.TypesOnly {
			typesOnly++
		}
		checked = append(checked, sourceFile)
	}
	return checked, typesOnly
}
