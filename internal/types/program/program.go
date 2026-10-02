// Package program builds the type graph and hands the other phases a checker they call as a
// function.
//
// This is the claim the whole tool rests on. Standing in the checker's own language means
// `GetTypeAtLocation` is a function call rather than a subprocess: the tool this replaces spent
// 2.09s per run rebuilding a 9,530-file program in a separate process to answer four type
// questions, and 2.08s of a 2.25s run went to program construction rather than to rules.
//
// The program is built once, and every phase reads the same one.
package program

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/bundled"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/tsoptions"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
	"github.com/microsoft/TypeScript/tsc/shim/vfs/cachedvfs"
	"github.com/microsoft/TypeScript/tsc/shim/vfs/osvfs"
	"github.com/system-inc/cohere/internal/lint/configuration"
)

// Graph is a built type graph: the program, and the checkers that answer questions about it.
//
// A Graph is never returned half-built. Build either hands back a Graph whose program resolved, or
// an error — there is no state in between, because the failure this package exists to make
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

	// CompilerHost is the filesystem view the program was built through, kept because the
	// incremental machinery needs the same one.
	//
	// ReadBuildInfoProgram takes a host and a ParsedCommandLine, and it has to be the host the
	// program was built with rather than a fresh one: the build info's file paths are resolved
	// relative to it, and a second host with a different current directory or a cold cache
	// would resolve them somewhere else and report every file as changed. Silently, and in the
	// direction that looks like a cold cache rather than like a bug.
	CompilerHost compiler.CompilerHost

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
}

// configHost adapts a filesystem and a working directory to what tsconfig parsing wants.
//
// tsoptions.ParseConfigHost is two methods, and typescript-go's own `execute` package satisfies it
// with a much larger System interface it happens to already have. We do not have that interface and
// do not want it: the rest of System is process concerns — stdout, exit codes, the clock — that
// belong to a CLI rather than to a type graph.
type configHost struct {
	fs               vfs.FS
	currentDirectory string
}

func (h *configHost) FS() vfs.FS                  { return h.fs }
func (h *configHost) GetCurrentDirectory() string { return h.currentDirectory }

// Options controls how a graph is built.
type Options struct {
	// ConfigFileName is the tsconfig to build from. Relative paths resolve against CurrentDirectory.
	ConfigFileName string

	// CurrentDirectory is the working directory paths resolve against. Empty means the process's own.
	CurrentDirectory string

	// LibraryPath overrides where the lib.*.d.ts files come from.
	//
	// Empty means the ones embedded in our pinned typescript-go, which is the correct default: the
	// checker and its libs are one version, and mixing a newer lib into an older checker produces
	// hundreds of spurious TS2550/TS2554 (measured: 118 of them on the ahra tree). Point this at a
	// specific lib directory only when deliberately reproducing another toolchain's result.
	LibraryPath string

	// Checkers is how many checkers to create. Zero means the compiler's own default.
	//
	// It is the count that parallelizes the checking phase, not the graph build: construction is
	// parse and resolve, which this does not touch. Raising it trades memory for wall clock, since
	// each checker holds its own type tables.
	Checkers int

	// SingleThreaded forces one checker instead of one per core. Off by default; useful when a
	// measurement needs to be reproducible, or when a caller wants types from different files to be
	// comparable (types from two checkers cannot be mixed).
	SingleThreaded bool

	// Overlay is file contents that stand in for the disk's, by absolute path: an editor's unsaved
	// buffer, so the graph describes the text about to be saved rather than the text being replaced.
	// Nil reads everything from disk.
	Overlay map[string]string
}

// Build resolves a tsconfig and constructs the program and its checkers.
//
// Every failure here is loud. A tsconfig that does not exist, one that does not parse, one whose
// options are contradictory, and one that matches no files are four different errors rather than
// four flavors of an empty result, because the whole point of this package is that a run which
// checked nothing must not be able to look like a run that found nothing.
func Build(options Options) (*Graph, error) {
	currentDirectory := options.CurrentDirectory
	if currentDirectory == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolving the current directory: %w", err)
		}
		currentDirectory = workingDirectory
	}
	currentDirectory = tspath.NormalizePath(currentDirectory)

	if options.ConfigFileName == "" {
		return nil, errors.New("no tsconfig given: a type graph needs a config to know what belongs in it")
	}
	configFileName := options.ConfigFileName
	if !tspath.IsRootedDiskPath(configFileName) {
		configFileName = tspath.CombinePaths(currentDirectory, configFileName)
	}
	configFileName = tspath.NormalizePath(configFileName)

	// bundled.WrapFS overlays the embedded lib.*.d.ts files onto the real disk, so `lib.dom.d.ts`
	// resolves without anything being installed. cachedvfs memoizes stat and readdir: config
	// resolution and module resolution ask the same directories about the same files repeatedly, and
	// on a 9,530-file program that repetition is most of the syscall traffic.
	var fileSystem vfs.FS = cachedvfs.From(bundled.WrapFS(osvfs.FS()))
	if len(options.Overlay) > 0 {
		fileSystem = newOverlayFS(fileSystem, options.Overlay)
	}

	host := &configHost{fs: fileSystem, currentDirectory: currentDirectory}

	if !fileSystem.FileExists(configFileName) {
		return nil, fmt.Errorf("no tsconfig at %s", configFileName)
	}

	// A nil extended-config cache is supported upstream and correct for a one-shot build: the cache
	// only pays off across repeated parses of the same `extends` chain within one process.
	config, configErrors := tsoptions.GetParsedCommandLineOfConfigFile(configFileName, &core.CompilerOptions{}, nil, host, nil)
	if len(configErrors) > 0 {
		return nil, fmt.Errorf("reading %s: %w", configFileName, joinDiagnostics(configErrors))
	}
	if config == nil {
		return nil, fmt.Errorf("reading %s: the config parsed to nothing", configFileName)
	}

	// A malformed option arrives on the config rather than in the returned error slice, and the two
	// channels do not overlap: an invalid `target` produces zero returned errors and one
	// GetConfigFileParsingDiagnostics entry. Checking only the slice — which is what reading the
	// signature suggests — means a config with a typo builds a program against silently wrong options
	// and reports whatever that produces as truth.
	if configFileDiagnostics := config.GetConfigFileParsingDiagnostics(); len(configFileDiagnostics) > 0 {
		return nil, fmt.Errorf("reading %s: %w", configFileName, joinDiagnostics(configFileDiagnostics))
	}

	// The config resolving to zero files is a configuration error, not a clean tree. Catching it here
	// means the caller never has to distinguish "nothing to check" from "nothing was checked", which
	// is the distinction every silent-green failure in this project's history collapsed.
	if len(config.FileNames()) == 0 {
		return nil, fmt.Errorf("%s matched no files: check its include, files, and exclude", configFileName)
	}

	libraryPath := options.LibraryPath
	if libraryPath == "" {
		libraryPath = bundled.LibPath()
	}
	// The last three arguments arrived with the move to `microsoft/TypeScript`. Nil is correct for all
	// three here: the extended-config cache only pays off across repeated parses of the same `extends`
	// chain within one process, `trace` is nil-guarded upstream and we have nowhere to route compiler
	// tracing, and the content mapper serves the language server rather than a one-shot check.
	compilerHost := compiler.NewCachedFSCompilerHost(currentDirectory, fileSystem, libraryPath, nil, nil, nil)

	singleThreaded := core.TSUnknown
	if options.SingleThreaded {
		singleThreaded = core.TSTrue
	}

	// The count goes onto the compiler options rather than being tracked beside them, so the compiler
	// and Workers() read the same number from the same place. A count kept alongside is a count that
	// can drift from the one the checkers were actually built with.
	checkerCount := options.Checkers
	if checkerCount <= 0 {
		checkerCount = defaultCheckerCount()
	}
	config.CompilerOptions().Checkers = &checkerCount

	// JSDocParsingMode used to be set here to parse JSDoc only where it can carry types. The option
	// was removed from ProgramOptions in the move to `microsoft/TypeScript` — the compiler now decides
	// per file rather than taking a program-wide mode — so there is nothing to pass and nothing to
	// preserve.
	builtProgram := compiler.NewProgram(compiler.ProgramOptions{
		Config:         config,
		Host:           compilerHost,
		SingleThreaded: singleThreaded,
	})
	if builtProgram == nil {
		return nil, fmt.Errorf("building a program from %s produced nothing", configFileName)
	}

	// The same guard one layer down, and it is not redundant with the one above: the config can match
	// files that the program then fails to load. Both are loud, and neither is an empty success.
	if len(builtProgram.GetSourceFiles()) == 0 {
		return nil, fmt.Errorf("the program built from %s contains no source files", configFileName)
	}

	return &Graph{
		Program:        builtProgram,
		Config:         config,
		ConfigFileName: configFileName,
		CompilerHost:   compilerHost,
	}, nil
}

// SourceFiles is every file in the program, third-party declarations included.
//
// This is the program's population, not the set a phase should lint. Use ProjectFiles for that.
func (g *Graph) SourceFiles() []*ast.SourceFile {
	return g.Program.GetSourceFiles()
}

// ProjectFiles is the subset of the program that the tsconfig actually named — our code, without
// the declarations it pulled in.
//
// On the ahra tree that is roughly 3,400 files out of 9,530. The other 6,100 are `node_modules`
// `.d.ts` that the checker needs in order to answer questions and that no rule should visit: a rule
// walking them would cost most of the run to report findings against code nobody here can edit.
func (g *Graph) ProjectFiles() []*ast.SourceFile {
	rootPaths := make(map[tspath.Path]struct{}, len(g.Config.FileNames()))
	for _, fileName := range g.Config.FileNames() {
		rootPaths[toPath(fileName, g.Config.GetCurrentDirectory(), g.Config.UseCaseSensitiveFileNames())] = struct{}{}
	}

	files := make([]*ast.SourceFile, 0, len(rootPaths))
	for _, sourceFile := range g.Program.GetSourceFiles() {
		if _, isRoot := rootPaths[sourceFile.Path()]; isRoot {
			files = append(files, sourceFile)
		}
	}

	// A lookup that misses reports the same empty result as a config that named nothing, and this one
	// has already missed once: the compiler lowercases Path() on a case-insensitive filesystem while
	// the config keeps its original casing, so comparing them without canonicalizing matched zero of
	// 3,407 files. Measured, not imagined — the uncanonicalized form still yields 0 today.
	//
	// Build's zero-file guard does not cover it, because it sits upstream and passes: the config did
	// name its files. The failure is between naming and matching, and it is silent by construction.
	// So the count is asserted here rather than trusted, and a partial miss is as loud as a total one.
	if len(files) != len(rootPaths) {
		panic(projectFilesMismatchMessage(len(rootPaths), len(files)))
	}

	return files
}

// projectFilesMismatchMessage says what went wrong and where to look.
//
// Split out so a test can assert the wording. A panic reading only "mismatch" sends the next reader
// to the tsconfig, which is the one place the fault is not.
func projectFilesMismatchMessage(named int, matched int) string {
	return fmt.Sprintf(
		"program: the tsconfig named %d files but only %d matched the program's own paths. "+
			"This is a path-canonicalization mismatch, not an empty project: the compiler lowercases "+
			"Path() on a case-insensitive filesystem while the config keeps its original casing. "+
			"Linting the %d that matched would silently skip the other %d.",
		named, matched, matched, named-matched,
	)
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
func (g *Graph) CheckerForFile(ctx context.Context, sourceFile *ast.SourceFile) (*checker.Checker, func()) {
	return g.Program.GetTypeCheckerForFileExclusive(ctx, sourceFile)
}

// Diagnostics returns TypeScript's own findings for one file: syntax, then binding, then semantics.
//
// The order is the one `tsc` reports in and it matters. A file that does not parse produces
// cascading nonsense from the later phases, so when syntax fails we stop rather than bury the real
// error under fifty invented ones.
//
// Prefer AllDiagnostics for a whole-program check. Calling this in a loop over every file is more
// than three times slower for a reason that is invisible at the call site — see the note there.
func (g *Graph) Diagnostics(ctx context.Context, sourceFile *ast.SourceFile) []*ast.Diagnostic {
	syntactic := g.Program.GetSyntacticDiagnostics(ctx, sourceFile)
	if len(syntactic) > 0 {
		return syntactic
	}

	diagnostics := g.Program.GetBindDiagnostics(ctx, sourceFile)
	return append(diagnostics, g.Program.GetSemanticDiagnostics(ctx, sourceFile)...)
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
func (g *Graph) AllDiagnostics(ctx context.Context) []*ast.Diagnostic {
	syntactic := g.Program.GetSyntacticDiagnostics(ctx, nil)
	if len(syntactic) > 0 {
		return syntactic
	}

	diagnostics := g.Program.GetBindDiagnostics(ctx, nil)
	return append(diagnostics, g.Program.GetSemanticDiagnostics(ctx, nil)...)
}

// ConfigDiagnostics returns findings about the configuration itself rather than about any file —
// contradictory options, an unreachable lib, a target that does not exist.
func (g *Graph) ConfigDiagnostics(ctx context.Context) []*ast.Diagnostic {
	// GetOptionsDiagnostics was folded into GetProgramDiagnostics upstream: an option that contradicts
	// another is now reported with the rest of the program's own findings rather than through its own
	// accessor. The set is the same; only the door changed.
	diagnostics := g.Program.GetConfigFileParsingDiagnostics()
	diagnostics = append(diagnostics, g.Program.GetProgramDiagnostics()...)
	diagnostics = append(diagnostics, g.Program.GetGlobalDiagnostics(ctx)...)
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
func (g *Graph) Workers() int {
	if g.Program.SingleThreaded() {
		return 1
	}

	checkerCount := defaultCheckerCount()
	if requested := g.Program.Options().Checkers; requested != nil {
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
	return max(min(checkerCount, len(g.Program.GetSourceFiles()), 256), 1)
}

// defaultCheckerCount is how many checkers we ask for when nobody says otherwise.
//
// The compiler's own default is a fixed 4, which was chosen for a machine we are not on: on a
// 16-core box it leaves the checking phase at a 2.6x speedup where the hardware allows more, so the
// ceiling was the setting rather than the workload. Measured on the ahra tree, 9,982 files:
//
//	checkers   types    allocated
//	       4   942ms      2,150MB
//	       8   758ms      2,483MB
//	      16   686ms      2,944MB
//	      32   762ms      3,467MB
//
// The curve turns over after core count, so this tracks GOMAXPROCS rather than hardcoding 16: a
// fixed 16 would oversubscribe a 4-core machine for the same reason a fixed 4 undersubscribes this
// one. The diagnostic count was 2 at every setting, which is the part that makes the speed
// trustworthy — a checking phase that got faster by checking less reports fewer findings, and this
// one does not.
//
// Memory is the trade and it is real: 37% more allocated between 4 and 16. That is affordable on a
// developer machine and worth watching if this ever runs somewhere small.
func defaultCheckerCount() int {
	return max(runtime.GOMAXPROCS(0), 1)
}

// toPath normalizes a file name the way the compiler keys its file table, so a lookup by path finds
// the file the compiler stored rather than a near-miss that differs only in case or separators.
func toPath(fileName string, currentDirectory string, useCaseSensitiveFileNames bool) tspath.Path {
	return tspath.ToPath(fileName, currentDirectory, useCaseSensitiveFileNames)
}

// joinDiagnostics turns compiler diagnostics into one error, capped so a config with a hundred
// problems reports the first few rather than a wall.
func joinDiagnostics(diagnostics []*ast.Diagnostic) error {
	const shown = 5

	messages := make([]error, 0, min(len(diagnostics), shown)+1)
	for index, diagnostic := range diagnostics {
		if index == shown {
			messages = append(messages, fmt.Errorf("and %d more", len(diagnostics)-shown))
			break
		}
		messages = append(messages, fmt.Errorf("TS%d: %s", diagnostic.Code(), diagnostic.MessageText()))
	}
	return errors.Join(messages...)
}
