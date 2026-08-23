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

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/bundled"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/tsoptions"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/microsoft/typescript-go/shim/vfs"
	"github.com/microsoft/typescript-go/shim/vfs/cachedvfs"
	"github.com/microsoft/typescript-go/shim/vfs/osvfs"
)

// defaultCheckerCount matches what the compiler creates when nothing overrides it. It is a fixed
// four rather than one-per-core: each checker holds its own type tables, so the memory cost grows
// with the count while the benefit flattens once the walk is no longer the bottleneck.
const defaultCheckerCount = 4

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

	// checkerCount is how many checkers the program built, and therefore the most files that can be
	// checked at once. The compiler keeps this private, so we record what we asked for.
	checkerCount int
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

	// SingleThreaded forces one checker instead of one per core. Off by default; useful when a
	// measurement needs to be reproducible, or when a caller wants types from different files to be
	// comparable (types from two checkers cannot be mixed).
	SingleThreaded bool
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
	fileSystem := cachedvfs.From(bundled.WrapFS(osvfs.FS()))

	host := &configHost{fs: fileSystem, currentDirectory: currentDirectory}

	if !fileSystem.FileExists(configFileName) {
		return nil, fmt.Errorf("no tsconfig at %s", configFileName)
	}

	// A nil extended-config cache is supported upstream and correct for a one-shot build: the cache
	// only pays off across repeated parses of the same `extends` chain within one process.
	config, configErrors := tsoptions.GetParsedCommandLineOfConfigFile(configFileName, &core.CompilerOptions{}, host, nil)
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
	compilerHost := compiler.NewCachedFSCompilerHost(currentDirectory, fileSystem, libraryPath)

	singleThreaded := core.TSUnknown
	checkerCount := defaultCheckerCount
	if options.SingleThreaded {
		singleThreaded = core.TSTrue
		checkerCount = 1
	}

	// JSDocParsingModeParseForTypeErrors is what `tsc` itself uses for a non-emitting check: JSDoc is
	// parsed only where it can carry types, rather than everywhere. On a tree that is 70%
	// node_modules declarations this is the difference between parsing comments that matter and
	// parsing all of them.
	builtProgram := compiler.NewProgram(compiler.ProgramOptions{
		Config:           config,
		Host:             compilerHost,
		SingleThreaded:   singleThreaded,
		JSDocParsingMode: ast.JSDocParsingModeParseForTypeErrors,
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
		checkerCount:   checkerCount,
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
	return files
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
	return g.Program.GetTypeCheckerForFile(ctx, sourceFile)
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
	diagnostics := g.Program.GetConfigFileParsingDiagnostics()
	diagnostics = append(diagnostics, g.Program.GetOptionsDiagnostics(ctx)...)
	diagnostics = append(diagnostics, g.Program.GetGlobalDiagnostics(ctx)...)
	return diagnostics
}

// Workers is how many files can be checked at once.
//
// It matches the checker count the program built, because a file must be visited by the checker that
// owns it and there is no benefit to more goroutines than there are checkers to serve them.
func (g *Graph) Workers() int {
	return g.checkerCount
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
		messages = append(messages, fmt.Errorf("TS%d: %s", diagnostic.Code(), diagnostic.Message()))
	}
	return errors.Join(messages...)
}
