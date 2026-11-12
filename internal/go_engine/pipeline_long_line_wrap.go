package go_engine

import (
	"cmp"
	"slices"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/format"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_file"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
)

// loadWorkspace loads the workspace's packages, through the findings cache when the run has one, keeping go list's
// answer in its store when it has a store: under `--no-cache` a session reading the binary's entries restores the
// standard library and keeps nothing. Every load reads each file the run holds from its held bytes (held), which are
// the disk's after every write the fix phase made. When a phase reads the types it says on stderr that the load
// failed; a load made for the wrap alone leaves that to the wrap's own held note, since that run asked for no
// type-check or lint to miss. A package with no file in scope is loaded for what imports it and never analyzed
// (hasFileInScope). The editor's save loads its file's package alone, with what it imports (stdinPackage).
func (engine *pipeline) loadWorkspace(typesRead bool) (*load.Loaded, error) {
	loaded, loadError := engine.loadWorkspacePackages()
	engine.noteFailedLoad(loadError, typesRead)
	return loaded, loadError
}

// noteFailedLoad says on stderr that the load failed, when a phase reads the types (see loadWorkspace).
func (engine *pipeline) noteFailedLoad(loadError error, typesRead bool) {
	if loadError != nil && typesRead {
		engine.note("the load failed, so nothing was type-checked or linted: %v", loadError)
	}
}

// loadWorkspacePackages is loadWorkspace's load, which says nothing on stderr, so the first load can run beside gofmt's
// check, whose notes come first (loadBesideFix).
func (engine *pipeline) loadWorkspacePackages() (*load.Loaded, error) {
	engine.watch(preludeLoadBegan)
	workspace := engine.described.workspace
	caching := load.Caching{
		Held:          engine.held,
		GoEnvironment: engine.described.goEnvironment,
		Timing:        engine.timing,
		Analyzes:      engine.described.hasFileInScope,
		SharedReads:   engine.reads,
	}
	if engine.session != nil {
		caching.Restorer = engine.session
		if engine.session.HasStore() {
			caching.Listings = engine.session
		}
	}
	if engine.lintOverlaps() {
		caching.Begin = engine.beginSpeculativeLint
	}
	caching.RecordReads = engine.recordsReads()
	started := time.Now()
	defer func() { engine.hostLoading.Add(int64(time.Since(started))) }()
	return engine.loadPackages(loadRequest{environment: workspace.Environment(), caching: &caching})
}

// wrapAfterLoad is the fix phase's long-line wrap, run after the load and only over a load that found the types sound,
// since every fix is held while they are not (#46ern7e): a load that failed, or found a type error, wraps nothing, and
// gofmt's output stands. It wraps, in memory, each file gofmt formatted that holds a long line, through the hook the
// fix phase has always used (longLineWrap) and format's checks that the wrap is the same program and gofmt's own
// layout, writes each wrap it keeps, rereads the file, and adds what it did to the held fix record, under the
// long-line rule. The editor's save holds each wrap it keeps in place of writing it. When it wrote a file and a phase
// reads the types (typesRead), it loads again and returns that load, since every phase after it must read the bytes the
// run holds. It returns the load it was given otherwise: a run that asked for no types reads none, so a second load
// would feed nothing. A wrap of a file outside the root (engine.writeRoot) is withheld rather than written, and its
// long lines stay findings.
func (engine *pipeline) wrapAfterLoad(loaded *load.Loaded, loadError error, typesRead bool) (*load.Loaded, error) {
	if engine.fix == nil || !engine.fix.wraps {
		return loaded, loadError
	}
	started := time.Now()
	sources := []format.Source{}
	for _, file := range engine.fix.formatted {
		contents, read := engine.described.contents[file]
		if read && consistency_no_long_line.HasLongLine(contents) {
			sources = append(sources, format.Source{File: file, Contents: contents})
		}
	}
	engine.fix.elapsed += time.Since(started)
	if len(sources) == 0 {
		return loaded, loadError
	}
	if loadError != nil {
		engine.note("the long-line fix was held, since the load failed and a fix waits for sound types: %v", loadError)
		return loaded, loadError
	}
	if len(loaded.Diagnostics) > 0 {
		engine.note("the long-line fix was held, since the load found type errors and a fix waits for sound types")
		return loaded, loadError
	}
	started = time.Now()
	// The editor's save wraps in memory and writes nothing (runStdin).
	saves := engine.request.stdinPath != ""
	result := format.RunWrap(sources, format.Options{
		Write:          !saves,
		KeepOutput:     saves,
		Root:           engine.writeRoot,
		SingleThreaded: engine.request.switches["single-threaded"],
		Known:          engine.fix.known,
		Wrap:           engine.longLineWrap(),
	})
	if len(result.Changed) > 0 {
		// The pass beside the load is of the bytes the wrap replaced, and reads the held bytes reread writes, so it
		// stops before the first reread, and the load of the wrapped tree starts its own.
		engine.abandonSpeculation()
	}
	engine.fix.wrapKnown = result.Known
	engine.addWrapped(result.Changed)
	for _, changed := range result.Changed {
		engine.hold(changed)
	}
	for _, declined := range result.WrapDeclined {
		// Not a gap: the file holds gofmt's output, and the lint phase still reports every long line in it.
		engine.note("%s: its long lines were not wrapped, so they stay findings: %s", declined.File, declined.Reason)
	}
	for _, withheld := range result.Withheld {
		// A gap, which writeFix names: the wrap was kept and not written, and the lint phase reports its long lines.
		engine.withhold(withheld.File, withheld.RealPath)
	}
	engine.fix.elapsed += time.Since(started)
	if len(result.Changed) == 0 || !typesRead {
		return loaded, loadError
	}
	// The findings cache stays sound across the write: every package's key hashes the bytes of its files as they are on
	// disk, which now hold the wrap, so this load misses on the packages the wrap wrote and those that import them,
	// and restores every other one, as the next run would.
	return engine.loadWorkspace(typesRead)
}

// addWrapped adds the files the wrap wrote to the held fix record, each once: a file gofmt also rewrote keeps its entry
// and gains the wrap's count under the long-line rule, and any other is a file rewritten that gofmt left alone. The
// entries stay in the order of the files in scope, which is by name, as gofmt's alone were.
func (engine *pipeline) addWrapped(wrapped []format.ChangedFile) {
	for _, changed := range wrapped {
		engine.fix.record.FixesApplied += changed.WrappedLines
		index := slices.IndexFunc(engine.fix.record.ChangedFiles, func(entry contract.ChangedFile) bool {
			return entry.File == changed.File
		})
		if index >= 0 {
			engine.fix.record.ChangedFiles[index].FixedBy[consistency_no_long_line.Name] += changed.WrappedLines
			continue
		}
		engine.fix.record.FilesRewritten++
		engine.fix.record.ChangedFiles = append(engine.fix.record.ChangedFiles, contract.ChangedFile{
			File:      changed.File,
			FixedBy:   map[string]int{consistency_no_long_line.Name: changed.WrappedLines},
			Formatted: false,
		})
	}
	slices.SortStableFunc(engine.fix.record.ChangedFiles, func(left, right contract.ChangedFile) int {
		return cmp.Compare(left.File, right.File)
	})
}

// longLineWrap is the hook the wrap asks of each file: wrapLongLines, held to the long-file rule's limit while the
// settings leave that rule on, so a fix for one rule never creates another's finding.
func (engine *pipeline) longLineWrap() func(file string, formatted []byte) format.Wrapping {
	limit, guarded := engine.longFileLimit()
	return func(file string, formatted []byte) format.Wrapping {
		if !guarded {
			return wrapLongLines(formatted)
		}
		return heldToLongFileLimit(file, formatted, wrapLongLines(formatted), limit)
	}
}

// longFileLimit is the long-file rule's limit as the settings configure it, read from the registered analyzer's max
// option as the rule's own run reads it, and false when the registry lacks the rule or the settings turn it off.
func (engine *pipeline) longFileLimit() (int, bool) {
	if _, on := engine.severityOf(consistency_no_long_file.Name); !on {
		return 0, false
	}
	for _, registered := range engine.registered {
		if registered.Name == consistency_no_long_file.Name {
			return consistency_no_long_file.LimitOf(registered.Analyzer), true
		}
	}
	return 0, false
}
