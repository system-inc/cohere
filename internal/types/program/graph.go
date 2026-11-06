package program

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/tsoptions"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// Graph is a built type graph: the program, and the checkers that answer questions about it.
//
// A Graph is never returned half-built. Build either hands back a Graph whose program resolved, or
// an error. There is no state in between, because the failure this package exists to make
// impossible is a program that silently contains nothing. An empty file list is indistinguishable
// from a clean tree, and that exact confusion let the gate this tool replaces print green over zero
// files for days.
type Graph struct {
	// Program is the compiler's own program handle. Exposed because a rule context carries it and
	// some type questions are asked of the program rather than of a checker.
	Program *compiler.Program

	// Config is the resolved tsconfig this graph was built from, kept so a later snapshot can hash
	// what it was built against rather than guess.
	Config *tsoptions.ParsedCommandLine

	// ConfigFileName is the absolute path of the tsconfig that produced this graph.
	ConfigFileName string

	// Anchor is the directory the graph was built in, which every fingerprint names paths relative to, so
	// that two spellings of one directory fingerprint every file alike (#547dhjz). See PathAnchor.
	Anchor PathAnchor

	// CompilerHost is the filesystem view the program was built through, kept because the
	// incremental machinery needs the same one.
	//
	// ReadBuildInfoProgram takes a host and a ParsedCommandLine, and it has to be the host the
	// program was built with rather than a fresh one: the build info's file paths are resolved
	// relative to it, and a second host with a different current directory or a cold cache
	// would resolve them somewhere else and report every file as changed. Silently, and in the
	// direction that looks like a cold cache rather than like a bug.
	CompilerHost compiler.CompilerHost

	// contentPack is the pack the program read its files through, and readIdentities the stats it read them
	// under when there was none. See ReadUnchanged.
	contentPack    *ContentPack
	readIdentities *readIdentities

	// LintConfig decides which rules apply to which files, and which files are not linted at all.
	//
	// Nil means every rule applies to every file, which is what the tests and any caller predating
	// this layer expect. The command always loads a real one and fails loudly if it cannot, so the
	// permissive default is reachable only from a caller that chose it deliberately.
	LintConfig *configuration.Config

	// RuleOptions says how to decode each rule's configuration, and which rules cannot run without
	// it. Empty means no rule takes options, which is what a test that did not set one expects.
	RuleOptions configuration.OptionsRegistry

	// RegisteredRuleNames is every rule the registry holds, whether or not this run selected it.
	//
	// The walk reads it to tell a suppression naming an unknown rule from one naming a real rule that
	// is merely not running here. Nil turns that check off, which is what a test that did not set it
	// expects: without a registry every name would read as unknown.
	RegisteredRuleNames []string

	// CollectTimings turns on per-rule cost measurement for the next Walk.
	//
	// Off by default and guarded at every timing site, so an ordinary run reads no clocks and
	// allocates no accumulators. The instrument should cost nothing when nobody asked for it.
	CollectTimings bool

	// FindingsReuse, when set, lets Walk serve unchanged files' cacheable findings from the last run
	// and records this run's for the next. Nil walks everything, exactly as before it existed.
	FindingsReuse *FindingsReuse

	// Readiness, when set, measures every walked file against cohere:adamic for the run's readiness, whatever
	// the project's chain enables. Nil measures nothing and the walk is exactly as before. See Readiness.
	Readiness *Readiness

	// Shapes is every project file's shape for this run (Graph.Signatures), when the caller computed them.
	// Walk keys shape-keyed rules on them; without them it keys those rules on the type fingerprint.
	Shapes map[string]SignatureEntry

	// projectFiles is the program's files that the tsconfig named, matched once by Build and verified
	// complete there. ProjectFiles returns it rather than recomputing, so the check that every named
	// file made it into the program happens where a failure can be returned instead of panicked.
	projectFiles []*ast.SourceFile

	// YieldedFiles is how many files the tsconfig named that a nearer one owns, left out of ProjectFiles.
	// See Options.Yielded.
	YieldedFiles int

	// typeGraphOnce and typeGraphParts hold typeGraph's answer, computed once: the type and the shape
	// fingerprints both read it, and the program it describes never changes after Build.
	typeGraphOnce  sync.Once
	typeGraphParts typeGraphParts

	// WalkOnForeignCheckers walks every file on a checker other than its own: a test instrument, for holding a
	// stolen file's findings and type diagnostics to its home checker's. See walkQueue.
	WalkOnForeignCheckers bool

	// ContentKeyedShapes is how many project files Signatures keyed on their content, with no recorded
	// declaration signature to compute theirs from. See Signatures.
	ContentKeyedShapes int

	// signatureEmits is how many files Signatures emitted declarations for, read by tests that prove when
	// it does and does not.
	signatureEmits int

	// FusedCheck, when set, has the next Walk type-check each file it visits, just before the file's rules run,
	// and the types phase read the result rather than check the program itself. Nil walks without checking.
	FusedCheck *FusedCheck

	// retired is set when the graph has been replaced, by Retire. See there.
	retired atomic.Bool
}

// Retire marks a graph replaced: the fix phase rewrote files and a new graph was built from the new bytes.
//
// A check still running on this graph is left to finish rather than cancelled, and what it finds is never
// recorded into the run cache's types section, since it describes bytes that no longer exist. It is not
// cancelled because the compiler's whole-program check cannot be: a checker that sees its context end
// marks itself cancelled, the compiler then hands it the next file in its group, and GetGlobalDiagnostics
// panics on a compiler goroutine nothing here can recover. The gate itself died that way, a bare `cohere`
// in 5 of 6 runs once its fix phase rewrote a file before the early check finished (#sm79kfv).
func (graph *Graph) Retire() {
	graph.retired.Store(true)
}

// Retired reports whether Retire was called.
func (graph *Graph) Retired() bool {
	return graph.retired.Load()
}

// yield takes the files a nearer tsconfig owns out of ProjectFiles, and counts them.
func (graph *Graph) yield(yielded map[string]struct{}) {
	if len(yielded) == 0 {
		return
	}
	kept := graph.projectFiles[:0:0]
	for _, sourceFile := range graph.projectFiles {
		if _, isYielded := yielded[filepath.Clean(filepath.FromSlash(sourceFile.FileName().AsString()))]; isYielded {
			graph.YieldedFiles++
			continue
		}
		kept = append(kept, sourceFile)
	}
	graph.projectFiles = kept
}

// timedVerify is verifyProjectFiles against the disk, timed when the build is. A build over another
// filesystem asks that one, since the real disk knows nothing of its files.
func (graph *Graph) timedVerify(options Options) (rootsVerdict, []string) {
	inspect := inspectRootOnDisk
	if options.FileSystem != nil {
		inspect = func(fileName string) rootState {
			if options.FileSystem.FileExists(tspath.RootedFilePath(fileName)) {
				return rootReadable
			}
			return rootAbsent
		}
	}
	timing := options.Timing
	if timing == nil {
		return graph.verifyProjectFiles(inspect)
	}
	started := time.Now()
	verdict, missing := graph.verifyProjectFiles(inspect)
	timing.Verify += time.Since(started)
	return verdict, missing
}

// verifyProjectFiles matches the config's files to the program's, keeps the matches for ProjectFiles,
// and says what any missing ones mean.
func (graph *Graph) verifyProjectFiles(inspect func(string) rootState) (rootsVerdict, []string) {
	rootPaths := make(map[tspath.PathKey]string, len(graph.Config.FileNames()))
	for _, fileName := range graph.Config.FileNames() {
		rootPaths[toPath(
			fileName.AsString(),
			graph.Anchor.Root(),
			graph.Config.CaseSensitivity(),
		)] = fileName.AsString()
	}

	files := make([]*ast.SourceFile, 0, len(rootPaths))
	loaded := make(map[tspath.PathKey]struct{}, len(rootPaths))
	for _, sourceFile := range graph.Program.GetSourceFiles() {
		if _, isRoot := rootPaths[sourceFile.PathKey()]; isRoot {
			files = append(files, sourceFile)
			loaded[sourceFile.PathKey()] = struct{}{}
		}
	}
	graph.projectFiles = files

	missing := []string{}
	for path, fileName := range rootPaths {
		if _, isLoaded := loaded[path]; !isLoaded {
			missing = append(missing, fileName)
		}
	}
	sort.Strings(missing)
	return classifyMissingRoots(missing, inspect), missing
}

/*
 * ReadUnchanged reports whether text, the program's copy of a file, is the file's bytes now, so a reader can
 * use it instead of reading the file again (#q6dey77). Exact, by three checks:
 *   - the program read the file under a stat, its content pack's or its own, and a stat taken now matches it,
 *     so the file holds the bytes the program read;
 *   - those bytes were the program's copy exactly. The program reads through typescript-go's decodeBytes,
 *     which drops a UTF-8 byte order mark and decodes UTF-16 behind its mark, and returns every other file
 *     byte for byte. So the file must not start with a mark (#hkv1hgp). The recorded size must also be the
 *     copy's length, which no dropped mark leaves true and which costs no read, so a marked file is turned
 *     away before it is opened. The length alone isn't enough: UTF-16 heavy in characters that take three
 *     bytes in UTF-8 can decode to exactly its own size.
 * False on any doubt, so the reader reads the disk, as it would have.
 */
func (graph *Graph) ReadUnchanged(path string, text string) bool {
	if graph == nil {
		return false
	}
	var identity fileIdentity
	var known bool
	if graph.contentPack != nil {
		identity, known = graph.contentPack.readIdentity(path)
	} else {
		identity, known = graph.readIdentities.identity(path)
	}
	if !known || identity.size != int64(len(text)) {
		return false
	}
	if current, statted := statIdentity(path); !statted || current != identity {
		return false
	}
	return !startsWithByteOrderMark(path)
}

// SourceFiles is every file in the program, third-party declarations included.
//
// This is the program's population, not the set a phase should lint. Use ProjectFiles for that.
func (graph *Graph) SourceFiles() []*ast.SourceFile {
	return graph.Program.GetSourceFiles()
}

// ProjectFiles is the subset of the program that the tsconfig actually named: our code, without
// the declarations it pulled in.
//
// On the ahra tree that is roughly 3,400 files out of 9,530. The other 6,100 are `node_modules`
// `.d.ts` that the checker needs in order to answer questions and that no rule should visit: a rule
// walking them would cost most of the run to report findings against code nobody here can edit.
func (graph *Graph) ProjectFiles() []*ast.SourceFile {
	// Matched and verified once, by Build. A lookup that misses reports the same empty result as a
	// config that named nothing, and this one has missed twice: the compiler lowercases Path() on a
	// case-insensitive filesystem while the config keeps its casing (matched zero of 3,407), and a file
	// can be deleted between the config's glob and the program's read (matched 3,789 of 3,790, with many
	// writers sharing the tree). Build tells the two apart and fails on the first, so nothing here can
	// return a silently partial list.
	return graph.projectFiles
}

// CheckerForFile returns the checker that owns a file, and a function to release it.
//
// There are several checkers, one per worker, and a file belongs to exactly one of them. That
// ownership is not an optimization detail a caller may ignore: types obtained from two different
// checkers cannot be compared or mixed, so a rule must ask the checker that owns the file it is
// looking at. Asking for the wrong one produces answers that are individually plausible and jointly
// meaningless, which is the worst failure shape available here.
//
// The release function must be called, conventionally by defer.
func (graph *Graph) CheckerForFile(
	callerContext context.Context,
	sourceFile *ast.SourceFile,
) (*checker.Checker, func()) {
	return graph.Program.GetTypeCheckerForFileExclusive(callerContext, sourceFile)
}

// Diagnostics returns TypeScript's own findings for one file: syntax, then binding, then semantics.
//
// The order is the one `tsc` reports in and it matters. A file that does not parse produces
// cascading nonsense from the later phases, so when syntax fails we stop rather than bury the real
// error under fifty invented ones.
//
// Prefer AllDiagnostics for a whole-program check. Calling this in a loop over every file is more
// than three times slower for a reason that is invisible at the call site. See the note there.
func (graph *Graph) Diagnostics(callerContext context.Context, sourceFile *ast.SourceFile) []*ast.Diagnostic {
	syntactic := graph.Program.GetSyntacticDiagnostics(callerContext, sourceFile)
	if len(syntactic) > 0 {
		return syntactic
	}

	diagnostics := graph.Program.GetBindDiagnostics(callerContext, sourceFile)
	return append(diagnostics, graph.Program.GetSemanticDiagnostics(callerContext, sourceFile)...)
}

// AllDiagnostics returns TypeScript's findings for the whole program.
//
// This is not a convenience wrapper around Diagnostics in a loop, and the difference is a measured
// 3.7s versus 1.1s on a 3,408-file tree. The compiler's diagnostic entry points branch on whether a
// file was named: pass one and it checks that file lazily, on whichever single checker owns it; pass
// nil and it first runs CheckSourceFiles, which fans every checker out across all four workers at
// once. So a loop that asks file by file quietly serializes the most expensive phase in the tool
// onto one core, and it does it without any call looking wrong.
//
// The findings are identical either way. Only the wall clock differs, which is exactly what makes
// this the kind of mistake that survives review.
func (graph *Graph) AllDiagnostics(callerContext context.Context) []*ast.Diagnostic {
	return graph.AllDiagnosticParts(callerContext).All()
}

// AllDiagnosticParts is AllDiagnostics with its parts kept apart. See TypeDiagnosticParts.
func (graph *Graph) AllDiagnosticParts(callerContext context.Context) TypeDiagnosticParts {
	syntactic := graph.Program.GetSyntacticDiagnostics(callerContext, nil)
	if len(syntactic) > 0 {
		return TypeDiagnosticParts{Syntactic: syntactic}
	}
	return TypeDiagnosticParts{
		Bind:     graph.Program.GetBindDiagnostics(callerContext, nil),
		Semantic: graph.Program.GetSemanticDiagnostics(callerContext, nil),
	}
}

// ConfigDiagnostics returns findings about the configuration itself rather than about any file:
// contradictory options, an unreachable lib, a target that does not exist.
func (graph *Graph) ConfigDiagnostics(callerContext context.Context) []*ast.Diagnostic {
	// GetOptionsDiagnostics was folded into GetProgramDiagnostics upstream: an option that contradicts
	// another is now reported with the rest of the program's own findings rather than through its own
	// accessor. The set is the same; only the door changed.
	diagnostics := graph.Program.GetConfigFileParsingDiagnostics()
	diagnostics = append(diagnostics, graph.Program.GetProgramDiagnostics()...)
	diagnostics = append(diagnostics, graph.Program.GetGlobalDiagnostics(callerContext)...)
	return diagnostics
}

// Workers is how many files can be checked at once.
//
// It matches the checker count the program built, because a file must be visited by the checker that
// owns it and there is no benefit to more goroutines than there are checkers to serve them.
//
// This asks the program rather than remembering what we requested. Until the move to
// `microsoft/TypeScript`, `Program.SingleThreaded()` was unexported and the checker count was a
// private constant, so this had to mirror the compiler's own arithmetic and hope the two stayed in
// step. Both are reachable now, so the mirror is gone: a count that disagreed with the compiler's
// would hand files to a worker whose checker does not own them, and types from two checkers cannot
// be mixed.
func (graph *Graph) Workers() int {
	if graph.Program.SingleThreaded() {
		return 1
	}

	checkerCount := defaultCheckerCount()
	if requested := graph.Program.Options().Checkers; requested != nil {
		checkerCount = *requested
	}

	// The clamp is upstream's, reproduced rather than approximated, because agreeing with the
	// compiler is the entire point of this function. It caps at the file count for the obvious reason
	// that a fifth checker on a four-file program has nothing to check, and at 256 as a ceiling.
	//
	// Without it a config asking for 100 checkers on a 64-file program gets 64 checkers upstream and
	// 100 workers here, and the 36 extra stride over files whose owning checker is a different
	// instance than the one they would be handed. Types from two checkers cannot be mixed, so that is
	// wrong answers rather than wasted goroutines. Measured: Workers() returned 100 against
	// upstream's 64 before this.
	return max(min(checkerCount, len(graph.Program.GetSourceFiles()), 256), 1)
}

// IsOptionsDiagnostic reports whether a diagnostic is about the compiler options rather than about
// source: a code from either list, located in no file or in one outside this program.
func (graph *Graph) IsOptionsDiagnostic(diagnostic *ast.Diagnostic) bool {
	_, raisedByProgram := optionsDiagnosticCodes[diagnostic.Code()]
	_, raisedByConfiguration := configParsingOptionCodes[diagnostic.Code()]
	if !raisedByProgram && !raisedByConfiguration {
		return false
	}
	sourceFile := diagnostic.File()
	return sourceFile == nil || graph.Program.GetSourceFile(sourceFile.FileName()) != sourceFile
}

// CheckReusing checks the project files whose shape fingerprint moved since the section was recorded and
// replays the rest. It reports false when it cannot, and the caller runs the full check and hands its parts
// to RecordFull: no section, a section under other compiler options or from a run that saw global
// diagnostics, no shapes this run, or so
// much changed that the full check, which spreads every checker over every file, is the faster way.
//
// Mutates: reuse records what was replayed and the section to save for the next run, unless the graph was retired
func (graph *Graph) CheckReusing(
	callerContext context.Context,
	reuse *TypeDiagnosticsReuse,
) (TypeDiagnosticParts, bool) {
	stored := reuse.stored
	if stored == nil ||
		stored.Version != typesSectionVersion ||
		stored.Key != reuse.key ||
		!stored.GlobalsClean ||
		graph.Shapes == nil {
		return TypeDiagnosticParts{}, false
	}
	syntactic := graph.Program.GetSyntacticDiagnostics(callerContext, nil)
	if len(syntactic) > 0 {
		return TypeDiagnosticParts{Syntactic: syntactic}, true
	}

	fingerprints := graph.SignatureFingerprints(graph.Shapes)
	projectFiles := graph.ProjectFiles()
	var replayed []*ast.Diagnostic
	var toCheck []*ast.SourceFile
	entries := make(map[string]TypesEntry, len(projectFiles))
	replayedFiles := map[string]struct{}{}
	for _, sourceFile := range projectFiles {
		entry, found := stored.Entries[sourceFile.FileName().AsString()]
		if found && entry.Fingerprint == fingerprints[sourceFile.PathKey()] {
			for _, diagnostic := range entry.Diagnostics {
				replayed = append(replayed, diagnostic.diagnostic(sourceFile))
			}
			entries[sourceFile.FileName().AsString()] = entry
			replayedFiles[sourceFile.FileName().AsString()] = struct{}{}
			continue
		}
		toCheck = append(toCheck, sourceFile)
	}
	if len(toCheck) > len(projectFiles)/2 {
		return TypeDiagnosticParts{}, false
	}

	bind := graph.Program.GetBindDiagnostics(callerContext, nil)
	checked := graph.semanticDiagnosticsOf(callerContext, toCheck)
	semantic := replayed
	for index, sourceFile := range toCheck {
		semantic = append(semantic, checked[index]...)
		if entry, keepable := typesEntryFor(sourceFile, fingerprints[sourceFile.PathKey()], checked[index]); keepable {
			entries[sourceFile.FileName().AsString()] = entry
		}
	}

	// A check cancelled part way can hand back a partial list, which recorded as a file's whole verdict would
	// replay a file as clean. Whatever finished before the cancel is still exactly the list for its key. A
	// retired graph's check describes bytes the fixer replaced, and its graph's successor records instead;
	// asked under the mutex, so a retired check finishing late can never overwrite what the successor
	// recorded.
	//
	// The replayed files too: a retired check finishing after its successor would otherwise leave the old
	// graph's count on the types line.
	reuse.mutex.Lock()
	if !graph.Retired() {
		reuse.replayed, reuse.replayedFiles = len(replayedFiles), replayedFiles
	}
	if callerContext.Err() == nil && !graph.Retired() {
		reuse.recorded = &TypesSection{Version: typesSectionVersion, Key: reuse.key, Entries: entries}
	}
	reuse.mutex.Unlock()
	return TypeDiagnosticParts{Bind: bind, Semantic: compiler.SortAndDeduplicateDiagnostics(semantic)}, true
}

// RecordFull records a full check's semantic diagnostics per project file, for the next run to replay.
//
// Mutates: reuse records the section to save for the next run, unless the graph was retired
func (graph *Graph) RecordFull(callerContext context.Context, reuse *TypeDiagnosticsReuse, parts TypeDiagnosticParts) {
	if len(parts.Syntactic) > 0 || graph.Shapes == nil || callerContext.Err() != nil {
		return
	}
	fingerprints := graph.SignatureFingerprints(graph.Shapes)
	byFile := map[*ast.SourceFile][]*ast.Diagnostic{}
	for _, diagnostic := range parts.Semantic {
		byFile[diagnostic.File()] = append(byFile[diagnostic.File()], diagnostic)
	}
	entries := make(map[string]TypesEntry, len(graph.ProjectFiles()))
	for _, sourceFile := range graph.ProjectFiles() {
		if entry, keepable := typesEntryFor(
			sourceFile,
			fingerprints[sourceFile.PathKey()],
			byFile[sourceFile],
		); keepable {
			entries[sourceFile.FileName().AsString()] = entry
		}
	}
	reuse.mutex.Lock()
	// Asked here, under the mutex, for the reason CheckReusing gives: a retired graph never records.
	if !graph.Retired() {
		reuse.recorded = &TypesSection{Version: typesSectionVersion, Key: reuse.key, Entries: entries}
	}
	reuse.mutex.Unlock()
}

// semanticDiagnosticsOf checks each file on the checker that owns it, a goroutine per checker, so a handful
// of files costs a handful of checks and many files still spread across every checker.
func (graph *Graph) semanticDiagnosticsOf(callerContext context.Context, files []*ast.SourceFile) [][]*ast.Diagnostic {
	results := make([][]*ast.Diagnostic, len(files))
	groups := map[*checker.Checker][]int{}
	for index, sourceFile := range files {
		owner, release := graph.Program.GetTypeCheckerForFile(callerContext, sourceFile)
		release()
		groups[owner] = append(groups[owner], index)
	}
	var waitGroup sync.WaitGroup
	for _, indices := range groups {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for _, index := range indices {
				results[index] = graph.Program.GetSemanticDiagnostics(callerContext, files[index])
			}
		}()
	}
	waitGroup.Wait()
	return results
}

// Walk visits every file in the given set once, dispatching every rule's listeners as it goes.
//
// One traversal serves every rule. This is the arrangement the whole tool exists to make possible:
// a rule that wanted its own pass would multiply the only unavoidable cost in the system by the
// number of rules, which is exactly how 53 rules of ordinary shape came to cost 1.81s. Here the
// five-hundredth rule costs a function call per node it asked about, and nothing at all for the nodes
// it did not.
//
// Files are walked in parallel, one goroutine per checker, because a file must be visited by the
// checker that owns it and there is no use in more workers than there are checkers.
func (graph *Graph) Walk(callerContext context.Context, files []*ast.SourceFile, rules []rule.Entry) (Result, error) {
	if len(files) == 0 {
		return Result{}, fmt.Errorf(
			"nothing to walk: the file set is empty (the program holds %d files)",
			len(graph.Program.GetSourceFiles()),
		)
	}
	if len(rules) == 0 {
		return Result{}, fmt.Errorf("nothing to run: no rules were given for %d files", len(files))
	}

	workers := min(graph.Workers(), len(files))

	// Built once per walk, read by every worker. Nil when no registry was supplied.
	catalog := newRuleNameCatalog(graph.RegisteredRuleNames, graph.LintConfig.RuleKeys())

	var mutex sync.Mutex
	diagnostics := []rule.Diagnostic{}
	filesReplayed, typeAwareRerun, shapeKeyedRerun, designSystemRerun, derivedRerun := 0, 0, 0, 0, 0
	filesOnForeignCheckers := 0

	// Computed once per walk, before any worker runs, and only when the findings cache is in use: about
	// 47ms on ahra, the one cost type-aware caching adds over the walk. Shape fingerprints need this run's
	// shapes, which the caller computes; without them the shape-keyed rules are keyed on the type
	// fingerprint instead, which can only re-run them more often.
	//
	// The derived rules' program fingerprints depend on each rule's options, so they are made on first ask, once
	// per rule and options for the whole walk, and every worker shares them. See programFingerprintMemo.
	programFingerprints := &programFingerprintMemo{entries: map[programFingerprintKey]*programFingerprintEntry{}}
	var fingerprints map[tspath.PathKey][sha256.Size]byte
	var shapeFingerprints map[tspath.PathKey][sha256.Size]byte
	if graph.FindingsReuse != nil && !graph.CollectTimings {
		fingerprints = graph.TypeFingerprints()
		shapeFingerprints = fingerprints
		if graph.Shapes != nil {
			shapeFingerprints = graph.SignatureFingerprints(graph.Shapes)
		}
	}
	listeningCounts := make(map[string]int, len(rules))
	reportingCounts := make(map[string]int, len(rules))
	offeredCounts := make(map[string]int, len(rules))
	nodesVisited, nodesReplayed := 0, 0
	suppressed := suppressionTally{}
	filesIgnored := 0
	scopedOff := map[string]int{}
	unconfigured := map[string]int{}
	unrunRuleReferences := map[string]int{}
	configurationFailures := []error{}
	fileCrashes := []FileCrash{}
	ruleCrashesAll := []RuleCrash{}
	notes := map[string]RuleNotes{}
	var adamic map[string]*AdamicRecord
	if graph.Readiness != nil {
		adamic = map[string]*AdamicRecord{}
	}

	// Nil unless asked for, and every timing call below is guarded on it, so a run without --timing
	// does not pay for the instrument at all.
	var timings *Timings
	if graph.CollectTimings {
		ruleNames := make([]string, 0, len(rules))
		for _, subject := range rules {
			ruleNames = append(ruleNames, subject.Name)
		}
		timings = NewTimings(ruleNames)
	}

	// Each worker walks the files of one checker, and then helps finish the others. See assignFilesToWorkers
	// and walkQueue.
	// A fused check needs each worker on one checker's files whether or not a rule reads one.
	needsCheckers := anyRuleNeedsTypeChecker(rules) || graph.FusedCheck != nil
	assignments := graph.assignFilesToWorkers(callerContext, files, workers, needsCheckers)
	queues := make([]*walkQueue, workers)
	homeFiles := make([]*ast.SourceFile, workers)
	for worker, indices := range assignments {
		queues[worker] = &walkQueue{indices: indices, back: len(indices)}
		if len(indices) > 0 {
			homeFiles[worker] = files[indices[0]]
		}
	}
	// The process's CPU when the walk began, so the timing table can say what the rest of the process
	// spent while the workers walked. Read only under --timing.
	var processStart time.Duration
	var processKnown bool
	if timings != nil {
		processStart, processKnown = processCpu()
	}

	var waitGroup sync.WaitGroup
	for worker := range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			localDiagnostics := []rule.Diagnostic{}
			localReplayed := 0
			localTypeAwareRerun := 0
			localShapeKeyedRerun := 0
			localDesignSystemRerun := 0
			localDerivedRerun := 0
			localListening := make(map[string]int, len(rules))
			localReporting := make(map[string]int, len(rules))
			localOffered := make(map[string]int, len(rules))
			localNodes, localNodesReplayed := 0, 0
			localSuppressed := suppressionTally{}
			localIgnored := 0
			localScopedOff := map[string]int{}
			localUnconfigured := map[string]int{}
			// The rule selection for each distinct resolution this worker has met. See rulesFor.
			localSelections := map[*configuration.ResolutionIdentity]*ruleSelection{}
			localFailures := []error{}
			localCrashes := []FileCrash{}
			localRuleCrashes := []RuleCrash{}
			localNotes := map[string]RuleNotes{}
			localAdamic := map[string]*AdamicRecord{}
			localForeign := 0

			// Each worker accumulates locally and merges once under the mutex. Timing through a
			// shared lock would measure contention rather than rule cost.
			var localTimings *Timings
			if timings != nil {
				localTimings = NewTimings(nil)
			}
			// Under --timing, this worker's thread CPU clock, read around every call into a rule. See
			// Timings. Nil otherwise.
			meter := startWorkerMeter(localTimings)

			// Made once for the worker rather than once per file, like everything the dispatcher holds.
			report := func(diagnostic rule.Diagnostic) {
				localDiagnostics = append(localDiagnostics, diagnostic)
				localReporting[diagnostic.RuleName]++
			}
			dispatcher := newFileDispatcher(graph, localTimings, catalog)

			// walkFile walks one file on the checker that owns checkerFile.
			walkFile := func(index int, checkerFile *ast.SourceFile) {
				if checkerFile != files[index] {
					localForeign++
				}
				sourceFile := files[index]

				// The type check first, on the checker the file is walked on, before this worker takes a checker
				// for the rules: the check takes that lock itself. See FusedCheck.
				if graph.FusedCheck != nil {
					graph.FusedCheck.checkFile(callerContext, graph, sourceFile, checkerFile)
				}

				// Configuration is consulted before a checker is acquired, because an ignored file
				// should cost nothing at all rather than cost a checker and then be discarded.
				selection, resolution := graph.rulesFor(
					sourceFile.FileName().AsString(),
					rules,
					localSelections,
					localScopedOff,
					localUnconfigured,
				)
				if resolution.Ignored {
					localIgnored++
					return
				}
				if selection.err != nil {
					// A rule that requires an option and did not get one is a hard failure, never a
					// quiet decline. Declining is indistinguishable from finding nothing, and that
					// ambiguity kept a dead rule alive for months.
					localFailures = append(localFailures, selection.err)
					return
				}
				if len(selection.applicable) == 0 {
					return
				}

				// The findings cache. A file whose bytes, key and applied cacheable rules all match the
				// last run's entry is walked with only its uncacheable rules, and the cacheable rules'
				// findings and coverage are replayed. Every other file is walked in full and, if it is
				// eligible, recorded. Off under --timing, since per-rule cost comes from the walk and a
				// replayed rule would time as free. See FindingsReuse.
				reuse := graph.FindingsReuse
				if graph.CollectTimings {
					reuse = nil
				}
				//
				// Type-aware rules replay only while their fingerprint is unchanged too, so an importer of an
				// edited file replays its pure rules and runs its type-aware ones again. Shape-keyed rules are
				// keyed on the shape fingerprint, which an edit inside an imported body does not move.
				//
				// Design-system rules replay while every path the design system read last time still holds and the
				// entry was produced under that design system. See DesignSystemKey.
				walkRules, walkSlots := selection.applicable, dispatcher.slotsFor(selection)
				var replayed *LintCacheEntry
				var replayedNotes RuleNotes
				var replayedRecord *AdamicRecord
				var hits classHits
				var keys cacheKeys
				var replays map[string]bool
				if reuse != nil {
					classes := selection.classNames()
					keys = cacheKeys{
						contentHash:      HashContent(sourceFile.Text()),
						pure:             classes.pure,
						typed:            classes.typed,
						typeFingerprint:  fingerprints[sourceFile.PathKey()],
						shaped:           classes.shaped,
						shapeFingerprint: shapeFingerprints[sourceFile.PathKey()],
						design:           classes.design,
						derived:          classes.derived,
					}
					keys.derivedFingerprint = derivedKey(
						classes,
						graph.programFingerprints(selection, programFingerprints),
						keys.typeFingerprint,
					)
					if entry, found := reuse.lookup(
						sourceFile.FileName().AsString(),
						keys,
						sourceFile.Text(),
					); found.pure {
						replayed = &entry
						hits = found
						replays = make(map[string]bool, len(selection.applicable))
						for _, names := range [][]string{
							keys.pure,
							classIf(hits.typed, keys.typed),
							classIf(hits.shaped, keys.shaped),
							classIf(hits.design, keys.design),
							classIf(hits.derived, keys.derived),
						} {
							for _, name := range names {
								replays[name] = true
							}
						}
						// The rules still to walk, in the order they were configured: the order rules run in
						// can decide which findings exist (HashRuleSet).
						walkRules, walkSlots = nil, nil
						for index, subject := range selection.applicable {
							if !replays[subject.Name] {
								walkRules = append(walkRules, subject)
								walkSlots = append(walkSlots, selection.slots[index])
							}
						}
						localReplayed++
						if !hits.typed && len(keys.typed) > 0 {
							localTypeAwareRerun++
						}
						if !hits.shaped && len(keys.shaped) > 0 {
							localShapeKeyedRerun++
						}
						if !hits.design && len(keys.design) > 0 {
							localDesignSystemRerun++
						}
						if !hits.derived && len(keys.derived) > 0 {
							localDerivedRerun++
						}
						replayedNotes = replayEntry(
							entry,
							hits,
							sourceFile,
							&localDiagnostics,
							localReporting,
							localOffered,
							localListening,
						)
						if graph.Readiness != nil {
							replayedRecord = replayedAdamic(entry, hits)
						}
						// A file with directives is dispatched even with nothing to walk, so its directives are read,
						// marked with what the replayed rules withheld, and tallied, as a walk of every rule does.
						if len(walkRules) == 0 && !entry.Directives {
							localNodesReplayed += entry.VisitedNodes
							if len(replayedNotes) > 0 {
								localNotes[sourceFile.FileName().AsString()] = replayedNotes
							}
							if graph.Readiness != nil {
								localAdamic[sourceFile.FileName().AsString()] = replayedRecord
							}
							return
						}
					}
				}

				// The checker is acquired only when an applicable rule declares it reads one. It is the checker that
				// owns checkerFile: the file itself, or for a file taken from another worker's group, a file of this
				// worker's own, so a stolen file is walked on this worker's checker. See walkQueue.
				//
				// CheckerForFile hands out an exclusive lock held until release, and it is held across
				// the whole dispatch below rather than around a single query.
				//
				// This comment used to say that acquiring it serializes this file's entire walk against
				// every other file's, at about 50% of the lint phase. That was measured and is wrong,
				// and the correction matters because the wrong version sends the next reader hunting a
				// bottleneck that is not there. Instrumented on 2026-08-24, per worker against a 2.45s
				// phase: 1,306ms waiting to acquire and 834ms holding and working, so 53% wait, which
				// reads like confirmation of the old number and is not.
				//
				// High wait does not mean lost throughput, because while one worker waits the other
				// fifteen work. The comparison that settles it is `--single-threaded`: 12.68-13.52s
				// against 2.32-2.52s parallel, so parallel is 5.35x faster. If the lock serialized each
				// file's walk against every other's, those would be nearly equal.
				//
				// The honest statement of the cost is that the lock is why this gets 5.35x rather than
				// something closer to 16x. That lost headroom is real; it is not elapsed time.
				//
				// This is paid now. Measured against the live registry on 2026-08-25: 212 rules
				// registered, 44 of them declaring NeedsTypeChecker, so the guard acquires on any file
				// one of those 44 applies to rather than skipping every file. The comment here
				// previously read "93 rules run and none reads the checker", which was true when the
				// tsgolint adapter had not registered and became false the moment it did.
				//
				// Left as a measurement with its date rather than a standing claim: a count of rules
				// is exactly the kind of number that decays silently, and the previous version of this
				// sentence is the proof.
				//
				// Declared rather than lazy on purpose. A getter that acquired on first use would work
				// until two rules on one file both asked, and the second acquisition would happen inside
				// the first's window: a deadlock whose shape is invisible at both call sites. The
				// declaration is legible at both ends and cannot deadlock by being used twice.
				var fileChecker *checker.Checker
				release := func() {
					// Nothing to release: no rule here asked for a checker, so none was acquired.
				}
				if anyRuleNeedsTypeChecker(walkRules) {
					fileChecker, release = graph.CheckerForFile(callerContext, checkerFile)
				}

				// A rule's Report closure belongs to the rule's slot, so a rule cannot report under another
				// rule's name even by accident. See fileDispatcher.
				//
				// A file being recorded counts its listening and offered rules into maps of its own, so
				// the entry can say which cacheable rules listened on this file; they are merged into the
				// worker's totals at once, before the crash check, exactly as passing the totals in did.
				recording := reuse != nil &&
					(replayed == nil || !hits.typed || !hits.shaped || !hits.design || !hits.derived)
				listeningTarget, offeredTarget := localListening, localOffered
				var fileListening map[string]int
				var fileOffered map[string]int
				if recording {
					fileListening = make(map[string]int, len(walkRules))
					fileOffered = make(map[string]int, len(walkRules))
					listeningTarget, offeredTarget = fileListening, fileOffered
				}
				diagnosticsBefore := len(localDiagnostics)
				if replayed != nil {
					dispatcher.replayed = &directiveReplay{
						withheld: withheldBy(replayed.Withheld, replays),
						rules:    selection.applicable,
					}
				}
				visited, silenced, fileNotes, ruleCrashes, fileAdamic, crashed := dispatcher.dispatchFileSafely(
					sourceFile,
					report,
					walkRules,
					walkSlots,
					fileChecker,
					listeningTarget,
					offeredTarget,
					selection.options,
					selection.measureOnly,
					resolution,
				)

				release()

				if recording {
					for name, count := range fileListening {
						localListening[name] += count
					}
					for name, count := range fileOffered {
						localOffered[name] += count
					}
				}

				if crashed != nil {
					// One file is lost rather than the run. Without this, a panic in any rule on any
					// node kills the process: files are walked in goroutines, a panic in a goroutine
					// cannot be recovered by its parent, and nothing else in this codebase recovers.
					// Measured before this existed: one panic on the branch producing the tree's 128
					// findings ended the run at exit 2 with no lint line and no phases line, losing a
					// completed types phase along with it.
					//
					// Named rather than counted, and never swallowed. A file the linter could not
					// process is not a file with nothing to report, and the whole coverage line exists
					// to keep those two apart.
					localCrashes = append(
						localCrashes,
						FileCrash{FileName: sourceFile.FileName().AsString(), Cause: crashed},
					)
					return
				}

				// A rule that crashed lost its verdict on this file and the rest of the file stands. The file
				// is not recorded in the findings cache, so the next run walks it again and names the crash
				// again rather than replaying a verdict with a hole in it.
				localRuleCrashes = append(localRuleCrashes, ruleCrashes...)
				if len(ruleCrashes) > 0 {
					recording = false
				}

				// The walk counts nodes only when some rule listens, so a replayed file whose walked rules listened
				// to nothing visited none, and its recorded walk's count stands, as replayed. One that was walked
				// visited every node, which is the same count, walked.
				if replayed != nil && visited == 0 {
					localNodesReplayed += replayed.VisitedNodes
				} else {
					localNodes += visited
				}
				localSuppressed.add(silenced)
				// What the walk measured, and for a replayed file what the cache replayed for the rest. A replayed
				// part that is nil leaves the whole unmeasured.
				walkedRecord := fileAdamic.record()
				if graph.Readiness != nil {
					localAdamic[sourceFile.FileName().AsString()] = walkedRecord
					if replayed != nil {
						localAdamic[sourceFile.FileName().AsString()] = mergeAdamic(replayedRecord, walkedRecord)
					}
				}
				// The walked rules' notes beside the replayed rules' notes: the two are disjoint by rule.
				if allNotes := mergeNotes(replayedNotes, fileNotes); len(allNotes) > 0 {
					localNotes[sourceFile.FileName().AsString()] = allNotes
				}

				if recording && replayed == nil {
					if entry, eligible := recordableEntry(
						sourceFile,
						keys,
						localDiagnostics[diagnosticsBefore:],
						fileListening,
						fileNotes,
						walkedRecord,
						visited,
						silenced,
					); eligible {
						reuse.keep(entry)
					}
				}
				if recording && replayed != nil {
					if entry, eligible := refreshClasses(
						*replayed,
						keys,
						hits,
						localDiagnostics[diagnosticsBefore:],
						fileListening,
						fileNotes,
						walkedRecord,
						silenced,
					); eligible {
						reuse.keep(entry)
					}
				}
			}

			// This worker's own group first, then files from the end of whichever group has the most left, walked
			// on this worker's own checker. Without the second loop the walk ends when its heaviest group does:
			// measured quiet on ahra (#zqsdzbq), workers ended between 0.89s and 1.59s, the last waiting 0.79s on
			// a checker lock its own group's type check also wanted.
			own := queues[worker]
			if graph.WalkOnForeignCheckers {
				own = foreignQueue(queues, homeFiles, worker)
			}
			for index, ok := own.next(); ok; index, ok = own.next() {
				if graph.WalkOnForeignCheckers {
					walkFile(index, homeFiles[worker])
				} else {
					walkFile(index, files[index])
				}
			}
			// Off when every file is already on a foreign checker: stealing could hand a worker its own group.
			if homeFiles[worker] != nil && !graph.WalkOnForeignCheckers {
				for victim := busiestQueue(queues); victim != nil; victim = busiestQueue(queues) {
					if index, ok := victim.steal(); ok {
						walkFile(index, homeFiles[worker])
					}
				}
			}

			// Stopped before the lock, so waiting for it is not counted as the walk's.
			meter.finish()

			// Appended in whichever order the workers finish. The order is fixed once, after the wait
			// below, rather than here, where it would still depend on who took the lock first.
			mutex.Lock()
			diagnostics = append(diagnostics, localDiagnostics...)
			for name, count := range localOffered {
				offeredCounts[name] += count
			}
			for name, count := range localListening {
				listeningCounts[name] += count
			}
			for name, count := range localReporting {
				reportingCounts[name] += count
			}
			nodesVisited += localNodes
			nodesReplayed += localNodesReplayed
			suppressed.add(localSuppressed)
			filesIgnored += localIgnored
			for name, count := range localScopedOff {
				scopedOff[name] += count
			}
			for name, count := range localUnconfigured {
				unconfigured[name] += count
			}
			configurationFailures = append(configurationFailures, localFailures...)
			fileCrashes = append(fileCrashes, localCrashes...)
			ruleCrashesAll = append(ruleCrashesAll, localRuleCrashes...)
			for fileName, fileNotes := range localNotes {
				notes[fileName] = fileNotes
			}
			for fileName, record := range localAdamic {
				adamic[fileName] = record
			}
			timings.merge(localTimings)
			filesReplayed += localReplayed
			typeAwareRerun += localTypeAwareRerun
			shapeKeyedRerun += localShapeKeyedRerun
			designSystemRerun += localDesignSystemRerun
			derivedRerun += localDerivedRerun
			filesOnForeignCheckers += localForeign
			for name, count := range dispatcher.unrunRuleReferences {
				unrunRuleReferences[name] += count
			}
			mutex.Unlock()
		}()
	}
	waitGroup.Wait()
	timings.closeAccount(processStart, processKnown)

	// What the design system read, if any rule loaded it, so the cache can key this walk's design-system
	// findings on it. Asked of the program after the walk rather than of the rules during it: the design system
	// is built once per program, whichever file asked first.
	if graph.FindingsReuse != nil && !graph.CollectTimings {
		graph.FindingsReuse.NoteDesignSystem(rule.DesignSystemReads(graph.Program))
	}

	sortDiagnostics(diagnostics)

	if len(configurationFailures) > 0 {
		// One error is enough to stop the run: they are all the same misconfiguration seen once per
		// file, so reporting the first names the problem without printing it 3,408 times.
		return Result{}, fmt.Errorf("rule configuration: %w", configurationFailures[0])
	}

	return Result{
		Diagnostics:       diagnostics,
		Timings:           timings,
		FilesReplayed:     filesReplayed,
		TypeAwareRerun:    typeAwareRerun,
		ShapeKeyedRerun:   shapeKeyedRerun,
		DesignSystemRerun: designSystemRerun,
		DerivedRerun:      derivedRerun,

		FilesOnForeignCheckers: filesOnForeignCheckers,
		Notes:                  notes,
		Adamic:                 adamic,
		Coverage: Coverage{
			FilesInProgram: len(graph.Program.GetSourceFiles()),
			FilesWalked:    len(files),
			NodesVisited:   nodesVisited,
			NodesReplayed:  nodesReplayed,
			RulesRun:       rulesOffered(offeredCounts),
			RulesOffered:   offeredCounts,
			RulesListening: listeningCounts,
			RulesReporting: reportingCounts,

			Suppressed:                      suppressed.applied,
			SuppressedWithoutReason:         suppressed.appliedNoReason,
			UnusedSuppressions:              suppressed.unusedDirectives,
			UnusedSuppressionsForUnrunRules: suppressed.unusedForUnrunRule,
			DeadSuppressions:                suppressed.dead,

			FilesCrashed:      fileCrashes,
			RulesCrashed:      ruleCrashesAll,
			FilesIgnored:      filesIgnored,
			RulesScopedOff:    scopedOff,
			RulesUnconfigured: unconfigured,

			UnrunRuleReferences: unrunRuleReferences,
		},
	}, nil
}

// rulesFor narrows the rule set to those the configuration enables for one file, and decodes each
// one's options into the type that rule declares.
//
// The error return is the point. A rule that requires an option and does not receive one is a hard
// failure here rather than a rule that quietly declines every file. Declining is correct behavior
// for a misconfigured guard and completely indistinguishable from a rule with nothing to report,
// which is how boundary-no-project-import stayed enabled and inert for months under the previous
// gate.
//
// When no configuration is loaded every rule applies with nil options, which keeps existing callers
// and tests working unchanged. The command always loads a real config and fails loudly if it
// cannot, so that permissive path is reachable only from a caller that chose it.
//
// A selection depends on the file's resolution alone, so it is made once per distinct resolution and
// shared by every file that resolved the same way (see configuration.Resolved.Identity), through the
// worker's selections. Made per file, the applicable slice, the decoded options and the cache classes
// were about 1 GB of a cold ahra run's allocation, for a few dozen distinct answers (#9jpmqm9). The
// scoped-off and unconfigured counts are still counted per file, from the selection's names.
//
// Mutates: selections keeps each selection made here by its resolution, for later files that resolve the same way
// Mutates: scopedOff counts each rule the configuration scoped off for this file, for the caller's totals
// Mutates: unconfigured counts each rule left unconfigured for this file, for the caller's totals
func (graph *Graph) rulesFor(
	fileName string,
	rules []rule.Entry,
	selections map[*configuration.ResolutionIdentity]*ruleSelection,
	scopedOff map[string]int,
	unconfigured map[string]int,
) (*ruleSelection, configuration.Resolved) {
	if graph.LintConfig == nil {
		selection, made := selections[noConfiguration]
		if !made {
			selection = &ruleSelection{applicable: rules}
			selections[noConfiguration] = selection
		}
		return selection, configuration.Resolved{}
	}

	resolution := graph.LintConfig.Resolve(fileName)
	if resolution.Ignored {
		return nil, resolution
	}

	identity := resolution.Identity()
	selection, made := selections[identity]
	if identity == nil || !made {
		selection = graph.selectRules(rules, resolution)
		if identity != nil {
			selections[identity] = selection
		}
	}
	if selection.err != nil {
		return selection, resolution
	}

	for _, name := range selection.scopedOff {
		scopedOff[name]++
	}
	for _, name := range selection.unconfigured {
		unconfigured[name]++
	}
	return selection, resolution
}

// selectRules makes the selection for one resolution.
func (graph *Graph) selectRules(rules []rule.Entry, resolution configuration.Resolved) *ruleSelection {
	selection := &ruleSelection{
		applicable:  make([]rule.Entry, 0, len(rules)),
		options:     map[string]rule.Bound{},
		optionsKeys: map[string]string{},
	}
	for _, subject := range rules {
		status, _ := resolution.StatusOf(subject.Name)
		if graph.Readiness.Measures(subject.Name) {
			if status == configuration.StatusScopedOff || status == configuration.StatusUnconfigured {
				// Off for the chain and run for readiness: still counted as off below, since that is what
				// the chain says, and run at the set's own options.
				bound, decodeError := graph.Bind(subject, graph.Readiness.Options[subject.Name])
				if decodeError != nil {
					selection.err = decodeError
					return selection
				}
				selection.options[subject.Name] = bound
				selection.optionsKeys[subject.Name] = optionsKey(graph.Readiness.Options[subject.Name])
				if selection.measureOnly == nil {
					selection.measureOnly = map[string]bool{}
				}
				selection.measureOnly[subject.Name] = true
				selection.applicable = append(selection.applicable, subject)
			}
		}
		switch status {
		case configuration.StatusScopedOff:
			// Someone configured this rule off, here or tree-wide. Counted rather than dropped
			// silently, because a rule absent across a directory is otherwise indistinguishable
			// from a rule with nothing to report.
			selection.scopedOff = append(selection.scopedOff, subject.Name)
			continue
		case configuration.StatusUnconfigured:
			// Nobody has said whether this rule should run. That is a different fact from a
			// deliberate exclusion and it is counted separately, or a rule waiting on a decision
			// reads as one somebody already made.
			selection.unconfigured = append(selection.unconfigured, subject.Name)
			continue
		}

		raw := resolution.RawOptionsFor(subject.Name)
		bound, decodeError := graph.Bind(subject, raw)
		if decodeError != nil {
			selection.err = decodeError
			return selection
		}
		selection.options[subject.Name] = bound
		selection.optionsKeys[subject.Name] = optionsKey(raw)
		selection.applicable = append(selection.applicable, subject)
	}
	return selection
}

// Bind decodes subject's option elements through the run's registry and returns the rule bound to them. A
// rule the registry holds no entry for runs as its Unconfigured, as it ran with no options before: an empty
// registry is a walk where no rule takes options (Graph.RuleOptions), and Decode still refuses any element
// written for such a rule.
func (graph *Graph) Bind(subject rule.Entry, elements []json.RawMessage) (rule.Bound, error) {
	bound, decodeError := graph.RuleOptions.Decode(subject.Name, elements)
	if decodeError != nil {
		return rule.Bound{}, decodeError
	}
	if _, declared := graph.RuleOptions[subject.Name]; !declared {
		return subject.Unconfigured, nil
	}
	return bound, nil
}

// assignFilesToWorkers gives each worker the files of one checker, so no two workers ever want the same
// checker and the exclusive lock each file is walked under is never contended.
//
// Files used to be handed out by index stride, on the belief that the compiler assigns a file to a checker
// by its position in the file list, which would keep each worker mostly on one checker. It does not: the
// checker pool partitions files by import affinity, a balanced graph partition, so a stride sent every
// worker across every checker. Traced on a cold ahra run, 2026-10-03 (#zqsdzbq): workers spent 34.4s,
// summed, blocked acquiring a checker, about half of all their time in a walk of about 4.4s. Interleaved
// against the stride on the same tree, three rounds, the walk ran about 25% faster in wall time on the same
// CPU, with identical findings.
//
// Groups are as balanced as the compiler made them for type checking, by files and imports rather than by
// what the rules cost, so the slowest group sets the walk's end.
//
// A walk with no rule that reads a checker strides instead: asking which checker owns a file creates every
// checker, which such a walk never needs.
func (graph *Graph) assignFilesToWorkers(
	callerContext context.Context,
	files []*ast.SourceFile,
	workers int,
	needsCheckers bool,
) [][]int {
	assignments := make([][]int, workers)
	if !needsCheckers {
		for index := range files {
			assignments[index%workers] = append(assignments[index%workers], index)
		}
		return assignments
	}
	slots := map[*checker.Checker]int{}
	for index, sourceFile := range files {
		owner, release := graph.Program.GetTypeCheckerForFile(callerContext, sourceFile)
		release()
		slot, seen := slots[owner]
		if !seen {
			slot = len(slots) % workers
			slots[owner] = slot
		}
		assignments[slot] = append(assignments[slot], index)
	}
	return assignments
}

// programFingerprints is the selection's derived rules' program fingerprints, by name, each the walk's one
// fingerprint for that rule under the options the selection gives it (rule.ProgramFingerprint). An option can
// choose what a rule reads, so two selections giving one rule different options can fingerprint it
// differently (#s9k38p3). Gathered once per selection from the walk's memo, which computes each rule and
// options pair once, whatever the number of selections or workers. A fingerprint that panics is left out,
// which keys no file the selection serves: the rule's findings there neither replay nor record this run.
//
// Mutates: selection keeps the fingerprints gathered here, so its later files reuse them
func (graph *Graph) programFingerprints(
	selection *ruleSelection,
	memo *programFingerprintMemo,
) map[string][sha256.Size]byte {
	if selection.programFingerprints != nil {
		return selection.programFingerprints
	}
	derived := selection.classNames().derivedRules
	fingerprints := make(map[string][sha256.Size]byte, len(derived))
	for _, subject := range derived {
		if fingerprint, usable := memo.fingerprint(
			graph,
			boundIn(selection.options, subject),
			selection.optionsKeys[subject.Name],
		); usable {
			fingerprints[subject.Name] = fingerprint
		}
	}
	selection.programFingerprints = fingerprints
	return fingerprints
}
