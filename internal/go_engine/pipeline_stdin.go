package go_engine

import (
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// runStdin is the editor's save (#qtna8m4): what a run with `--fix` would write for the one file `--stdin-filepath`
// names once the rest of the workspace type-checks, computed from the buffer on standard input, written to output
// whole, with nothing written to disk. A save loads its file's package and what it imports, never the rest
// (stdinPackage), so where another package holds a type error, a run holds the long-line wrap and the save makes it:
// the save is ahead of the gate there, never against it. It is the fix phase of run over a scope of that one file,
// whose bytes are the buffer everywhere the run reads them (invocation.read), and no other phase, so no record is
// written:
//
//   - gofmt, in memory (format.Options.KeepOutput).
//   - the long-line wrap, in memory, over a load of the formatted buffer, and held as run holds it when the load
//     failed or found type errors, with the same notes on stderr.
//   - the renames the naming rules ask, never made, since a rename spans files: each one in this file is named on
//     stderr (nameRenamesLeft).
//
// The outcomes are TypeScript's stdin's (command/cohere/stdin.go), for the save hook to act on:
//
//   - The file's text on output, exit 0, whether or not anything changed.
//   - The buffer unchanged on output, exit 0, for a file the run declines: one the census leaves out, git's ignore
//     files or the build constraints, with the reason on stderr in the words the run's footer uses; one no package of
//     the root holds; and a buffer that does not parse. A save is not the place to report a typo; the gate reports it.
//   - Nothing on output, exit 1, for a formatter that broke, with why on stderr. The hook shows it and saves the buffer
//     as typed.
func (engine *pipeline) runStdin(output io.Writer) int {
	path := engine.request.stdinPath
	if _, held := engine.described.contents[path]; !held || !engine.described.inScopeSet[path] {
		engine.note("%s was left as typed: %s", path, engine.stdinDeclined(path))
		return writeStdin(output, engine.request.buffer, engine.standardError)
	}
	if _, parseError := parser.ParseFile(token.NewFileSet(), path, engine.request.buffer,
		parser.SkipObjectResolution); parseError != nil {
		engine.note("%s was left as typed: it does not parse: %v", path, parseError)
		return writeStdin(output, engine.request.buffer, engine.standardError)
	}

	wraps := engine.wrapsLongLines()
	engine.session = engine.cacheSession(engine.described.workspace.Environment(),
		engine.described.goEnvironment, cache.FileDigests{}, engine.standardError)
	engine.runFix(wraps)
	if engine.fix.record.FilesNotFormatted > 0 {
		// The buffer parses, so gofmt not formatting it is a formatter that broke, which runFix has said on stderr.
		return exitFindings
	}
	names := engine.renamesNames()
	var loaded *load.Loaded
	var loadError error
	if wraps || names {
		// Said on stderr when it fails and the renames would read it, as a run says it when a phase reads the types.
		loaded, loadError = engine.loadWorkspace(names)
	}
	// When the wrap changed the buffer and the renames are named, this is a load of the wrapped buffer, so a name is
	// spelled from the bytes its finding's position is in.
	loaded, loadError = engine.wrapAfterLoad(loaded, loadError, names)
	// Ignored: a save's rename only names what it leaves, and the error it returns is the load's, which the load
	// said on stderr already; the save answers with its buffer either way.
	engine.runRename(loaded, loadError)
	if engine.fix.known != nil {
		// A save looks up one file's verdict, and the record keeps only what a run read or learned, so kept as it is it
		// would drop every other file's and the next plain run would format them all again (#h6dvf3j). So a save keeps
		// every verdict it read beside what it learned; a plain run still drops the files long gone.
		engine.fix.known.KeepAll()
	}
	engine.keepKnownFiles(engine.fix.known)
	return writeStdin(output, engine.described.contents[path], engine.standardError)
}

// stdinPackage is what the editor's save loads: the pattern of its file's directory, `./sub` from the root, which
// names that one package with its tests' variants, as a load of the whole workspace names each package, and the
// census's files in that directory, which the load accounts for. A save judges its one file, so it never loads a
// package that imports it: each would key apart from its cache entry on the buffer's every edit and be type-checked
// again for nothing, which on a widely imported file is most of the workspace. Its imports are restored as a load of
// the workspace restores them.
//
// So the wrap's hold differs from a run's on purpose: a run holds the wrap when its load finds a type error anywhere in
// the workspace, and a save only when the error is in its file's package or what that imports. A save judges its file,
// and the gate, which loads the workspace, still holds the wrap over an error in an importer.
func (engine *pipeline) stdinPackage() ([]string, []string) {
	directory := filepath.Dir(engine.request.stdinPath)
	// Ignored: both paths are absolute, so Rel cannot fail, and a file in scope is below the root.
	relative, _ := filepath.Rel(engine.described.workspace.Root, directory)
	pattern := "."
	if relative != "." {
		pattern = "./" + filepath.ToSlash(relative)
	}
	var census []string
	for _, file := range engine.described.readableOurs {
		if filepath.Dir(file) == directory {
			census = append(census, file)
		}
	}
	return []string{pattern}, census
}

// stdinDeclined is why the editor's save holds nothing for its file: the reason the project record gives for leaving
// it out, with the rule that decided it, as `ignored by git (.gitignore:1)`, the words a run's footer says it in when
// that left nothing checked (#08cv2mp), or else that no package the run checks holds it.
func (engine *pipeline) stdinDeclined(path string) string {
	index := slices.IndexFunc(engine.described.record.Excluded, func(excluded contract.ExcludedFile) bool {
		return excluded.File == path
	})
	if index < 0 {
		return fmt.Sprintf("no package of the Go %s at %s holds it, so a run there checks no such file",
			engine.described.container(), engine.described.workspace.Root)
	}
	excluded := engine.described.record.Excluded[index]
	if excluded.Source == "" {
		return excluded.Reason
	}
	return excluded.Reason + " (" + excluded.Source + ")"
}

// writeStdin writes a save's answer, the file's text, whole, and says on stderr when it could not.
func writeStdin(output io.Writer, contents []byte, standardError io.Writer) int {
	if _, writeError := output.Write(contents); writeError != nil {
		// Ignored: stderr is the last place left to say it, and the exit code says it whether or not this lands.
		_, _ = fmt.Fprintf(standardError, "cohere-go: the file's text could not be written to standard output: %v\n",
			writeError)
		return exitBroken
	}
	return exitClean
}
