package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/ecmascript/canonical_function_hash"
	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/lint/suppression"
	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/internal/types/program"
)

// duplicatesRequested is `--duplicates`: a report beside --unused, never a gate (#jfvhbj7, @system_cohere's ruling on
// #6rj7jw7). Opt-in for unused's reason: it answers "was this written twice", which no build can enforce, so it never
// moves the exit code. Registered here rather than among run's flags, which is where every other flag is, only
// because main.go is at the house's length limit; flag.Parse in run reads it all the same.
var duplicatesRequested = flag.Bool("duplicates", false,
	"report functions that compute the same thing, written more than once: groups whose canonical IR is one graph, "+
		"size 11 and up, a report that never fails a build")

// directiveRuleNames are the names a suppression may give that the walk knows: every rule the registry holds, and the
// duplicates report's, which a suppression gives to keep a function in its shape. The walk never runs that one, so it
// neither applies nor dead-checks such a suppression; the report does both.
func directiveRuleNames() []string {
	return append(registry.Names(), duplicate_functions.TypeScriptRuleName)
}

// writeDuplicatesNotRun says, under `--duplicates`, that the report did not run and why, for a run that ends before
// it: a name that does not resolve would hash as a global, joining callees that differ.
func writeDuplicatesNotRun(reason string) {
	if !*duplicatesRequested {
		return
	}
	writeDuplicatesReport(streams.Output, duplicate_functions.Report{Floor: duplicate_functions.Floor,
		NotRun: reason + ", and a name that does not resolve would hash as a global, joining callees that differ"})
}

// writeTypeScriptDuplicates prints the duplicates report under `--duplicates`, after every phase, over the graph they
// checked. A run that built no graph, as `--format-only` builds none, says the report did not run. A run that neither
// fixes nor lints loaded no settings, so they are read here, since the report leaves out what they ignore, and
// settings that cannot be read say the report did not run rather than fail a run the report must not move.
func writeTypeScriptDuplicates(
	callerContext context.Context,
	graph *program.Graph,
	projectFiles []*ast.SourceFile,
	lintConfiguration *configuration.Configuration,
	location projectLocation,
) {
	if !*duplicatesRequested {
		return
	}
	if graph == nil {
		writeDuplicatesReport(streams.Output, duplicate_functions.Report{Floor: duplicate_functions.Floor,
			NotRun: "this run built no program to read functions from, as --format-only builds none"})
		return
	}
	settings := lintConfiguration
	if settings == nil {
		loaded, loadForError := configuration.LoadFor(location.LintConfigFileName, registeredRuleNames())
		if loadForError != nil {
			writeDuplicatesReport(streams.Output, duplicate_functions.Report{Floor: duplicate_functions.Floor,
				NotRun: fmt.Sprintf("reading %s: %v", location.LintConfigFileName, loadForError)})
			return
		}
		settings = loaded
	}
	writeDuplicatesReport(streams.Output, typeScriptDuplicates(callerContext, graph, projectFiles, settings, location))
}

// typeScriptDuplicates is the duplicates report over a TypeScript program: every function of every project file the
// settings do not ignore, hashed on the checker that owns its file (canonical_function_hash), grouped, with the
// suppressions naming duplicate_functions.TypeScriptRuleName applied.
//
// It runs after lint, over the graph the run checked, which is the one rebuilt after a fix rewrote a file. Lowering
// does not survive the walk (the IR's ForFunction keeps it per file, for the rules), so every function is lowered
// here again, except in a file whose entry in the cache table's duplicates section still holds: see
// DuplicatesSection for what an entry is keyed on.
func typeScriptDuplicates(
	callerContext context.Context,
	graph *program.Graph,
	projectFiles []*ast.SourceFile,
	lintConfiguration *configuration.Configuration,
	location projectLocation,
) duplicate_functions.Report {
	started := time.Now()
	report := duplicate_functions.Report{Ran: true, Cache: duplicate_functions.CacheUse{Unit: "files"}}
	var files []*ast.SourceFile
	for _, sourceFile := range projectFiles {
		if sourceFile.IsDeclarationFile {
			continue
		}
		if lintConfiguration != nil && lintConfiguration.Resolve(sourceFile.FileName().AsString()).Ignored {
			report.FilesIgnored++
			continue
		}
		files = append(files, sourceFile)
	}
	report.Files = len(files)

	cached := readDuplicatesSection(location)
	fingerprints := duplicatesFingerprints(graph)
	results := make([]canonical_function_hash.FileFunctions, len(files))
	var missed []*ast.SourceFile
	var missedIndex []int
	for index, sourceFile := range files {
		entry, hit := cached.Entries[sourceFile.FileName().AsString()]
		if hit && entry.Fingerprint == fingerprints[sourceFile.PathKey()] {
			results[index] = fromDuplicatesEntry(sourceFile.FileName().AsString(), entry)
			report.Cache.Read++
			continue
		}
		missed = append(missed, sourceFile)
		missedIndex = append(missedIndex, index)
	}
	report.Cache.Hashed = len(missed)
	var crashesLock sync.Mutex
	graph.EachFileOnItsChecker(callerContext, missed, func(
		index int,
		sourceFile *ast.SourceFile,
		typeChecker *checker.Checker,
	) {
		hashed, crash := hashFileRecovering(sourceFile, typeChecker, location.Root)
		if crash != nil {
			crashesLock.Lock()
			report.Crashes = append(report.Crashes, *crash)
			crashesLock.Unlock()
			return
		}
		results[missedIndex[index]] = hashed
	})
	if len(missed) > 0 {
		writeDuplicatesSection(location, files, results, fingerprints, report.Crashes)
	}

	var functions []duplicate_functions.Function
	for _, result := range results {
		functions = append(functions, result.Functions...)
		for _, refused := range result.Refused {
			report.Refuse(refused)
		}
		report.Skip(duplicate_functions.NotReadBodyless, result.Bodyless)
	}
	keepings := typeScriptKeepings(files)
	keeperOf := map[duplicate_functions.Place]*typeScriptKeeping{}
	for index := range functions {
		if keeper := keepingAt(keepings[functions[index].File], functions[index].Line); keeper != nil {
			functions[index].Kept = keptReason(keeper)
			keeperOf[duplicate_functions.Place{File: functions[index].File, Line: functions[index].Line}] = keeper
		}
	}
	for member := range report.Assemble(functions) {
		if keeper, kept := keeperOf[member]; kept {
			keeper.alive = true
		}
	}
	for file, inFile := range keepings {
		for _, keeping := range inFile {
			if !keeping.alive {
				report.DeadSuppressions = append(report.DeadSuppressions,
					duplicate_functions.Place{File: file, Line: keeping.directive.Line + 1})
			}
		}
	}
	report.SortRefused()
	report.ElapsedMilliseconds = int(time.Since(started).Milliseconds())
	return report
}

// hashFileRecovering hashes one file, and turns a panic in the hash into a crash the report names, so one file the
// IR cannot be walked through never ends the run that asked for a report.
func hashFileRecovering(
	sourceFile *ast.SourceFile,
	typeChecker *checker.Checker,
	root string,
) (hashed canonical_function_hash.FileFunctions, crash *duplicate_functions.Crash) {
	defer func() {
		if recovered := recover(); recovered != nil {
			crash = &duplicate_functions.Crash{File: sourceFile.FileName().AsString(),
				Error: fmt.Sprintf("%v\n%s", recovered, debug.Stack())}
		}
	}()
	return canonical_function_hash.HashFile(sourceFile, typeChecker, root), nil
}

// duplicatesFingerprints is each project file's fingerprint, the key of its duplicates entry: its signature
// fingerprint when the run computed shapes, as one with types does, and its type fingerprint otherwise.
func duplicatesFingerprints(graph *program.Graph) map[tspath.PathKey][sha256.Size]byte {
	if graph.Shapes != nil {
		return graph.SignatureFingerprints(graph.Shapes)
	}
	return graph.TypeFingerprints()
}

// typeScriptKeeping is one directive naming the duplicates report's rule, and whether it keeps a member of a group at
// the floor.
type typeScriptKeeping struct {
	directive *suppression.Directive
	alive     bool
}

// keptReason is what a keeping directive says, or that it says nothing.
func keptReason(keeping *typeScriptKeeping) string {
	if keeping.directive.Reason == "" {
		return "no reason given"
	}
	return keeping.directive.Reason
}

// typeScriptKeepings are the directives naming the report's rule by name, a blanket never, by file.
func typeScriptKeepings(files []*ast.SourceFile) map[string][]*typeScriptKeeping {
	bareName := duplicate_functions.TypeScriptRuleName[strings.LastIndex(duplicate_functions.TypeScriptRuleName, "/")+
		1:]
	keepings := map[string][]*typeScriptKeeping{}
	for _, sourceFile := range files {
		// Only a file that spells the name can hold a directive naming it, so the rest are never scanned.
		if !strings.Contains(sourceFile.Text(), bareName) {
			continue
		}
		for _, directive := range suppression.Build(sourceFile.Text()).Directives() {
			if len(directive.Rules) > 0 && directive.Names(duplicate_functions.TypeScriptRuleName) {
				name := sourceFile.FileName().AsString()
				keepings[name] = append(keepings[name], &typeScriptKeeping{directive: directive})
			}
		}
	}
	return keepings
}

// keepingAt is the directive keeping the function on a 1-based line: one covering that line alone before one
// covering the whole file, as suppression credits a line's own directive first.
func keepingAt(keepings []*typeScriptKeeping, line int) *typeScriptKeeping {
	for _, wholeFile := range []bool{false, true} {
		for _, keeping := range keepings {
			if (keeping.directive.Kind == suppression.KindFile) == wholeFile &&
				keeping.directive.Covers(duplicate_functions.TypeScriptRuleName, line-1) {
				return keeping
			}
		}
	}
	return nil
}

// readDuplicatesSection is the cache table's duplicates section under this project's key, or an empty one: under
// `--no-cache`, on a first run, or under another key.
func readDuplicatesSection(location projectLocation) *program.DuplicatesSection {
	empty := &program.DuplicatesSection{Entries: map[string]program.DuplicatesEntry{}}
	if cacheOff {
		return empty
	}
	prepareCacheDirectory(location.Root)
	directory := cache_directory.Of(location.Root)
	release, _ := holdTableReadLock(directory, tableWriterWait)
	anchor := program.NewPathAnchor(location.Root)
	// Ignored: a table missing or unreadable reads as empty, and the section is written whole after the hash.
	table, _ := program.ReadCacheTable(directory, cacheTableIdentity(), program.CacheTableSections{Duplicates: true},
		anchor)
	release()
	if table.Duplicates == nil || table.Duplicates.Key != duplicatesSectionKey(location) ||
		table.Duplicates.Entries == nil {
		return empty
	}
	return table.Duplicates
}

// writeDuplicatesSection writes the section anew with every file's entry this run, those read and those hashed,
// under the table's lock, so the next run hashes only what moved. A file whose hash crashed gets no entry. Nothing
// is written under `--no-cache`, and a write that fails is a note, never a failed run.
func writeDuplicatesSection(
	location projectLocation,
	files []*ast.SourceFile,
	results []canonical_function_hash.FileFunctions,
	fingerprints map[tspath.PathKey][sha256.Size]byte,
	crashes []duplicate_functions.Crash,
) {
	if cacheOff {
		return
	}
	crashed := map[string]bool{}
	for _, crash := range crashes {
		crashed[crash.File] = true
	}
	section := &program.DuplicatesSection{Key: duplicatesSectionKey(location),
		Entries: make(map[string]program.DuplicatesEntry, len(files))}
	for index, sourceFile := range files {
		name := sourceFile.FileName().AsString()
		if crashed[name] {
			continue
		}
		entry := program.DuplicatesEntry{Fingerprint: fingerprints[sourceFile.PathKey()],
			Bodyless: results[index].Bodyless}
		for _, function := range results[index].Functions {
			function.File = ""
			entry.Functions = append(entry.Functions, function)
		}
		for _, refused := range results[index].Refused {
			refused.File = ""
			entry.Refused = append(entry.Refused, refused)
		}
		section.Entries[name] = entry
	}
	directory := cache_directory.Of(location.Root)
	release := holdTableLock(directory)
	defer release()
	table := program.NewCacheTable()
	table.Duplicates = section
	if writeError := program.WriteCacheTable(directory, table, cacheTableIdentity(),
		program.CacheTableSections{Duplicates: true}, program.NewPathAnchor(location.Root)); writeError != nil {
		cacheNote(directory, fmt.Sprintf("the duplicates section could not be written: %v",
			firstLine(writeError.Error())))
	}
}

// fromDuplicatesEntry is a file's functions as its entry holds them, each named by the file again.
func fromDuplicatesEntry(file string, entry program.DuplicatesEntry) canonical_function_hash.FileFunctions {
	result := canonical_function_hash.FileFunctions{Bodyless: entry.Bodyless}
	for _, function := range entry.Functions {
		function.File = file
		result.Functions = append(result.Functions, function)
	}
	for _, refused := range entry.Refused {
		refused.File = file
		result.Refused = append(result.Refused, refused)
	}
	return result
}

// duplicatesSectionKey is what every entry of the section depends on beyond its own file: the tsconfig that decides
// how names resolve. The binary, which decides how a function is hashed, is in every table file's header.
func duplicatesSectionKey(location projectLocation) string {
	return "tsconfig=" + program.NewPathAnchor(location.Root).Stable(location.ConfigFileName)
}
