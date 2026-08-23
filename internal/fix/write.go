package fix

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/parser"
	"github.com/microsoft/typescript-go/shim/tspath"
)

// Parses reports whether text parses cleanly as the script kind implied by its file name.
//
// This is the refusal guard, and it is the single reason autofix can be trusted at all. A rule
// proposes bytes; nothing about a byte range guarantees the result is a program. A range computed
// one token off, a replacement missing a closing brace, two fixes that are individually fine and
// jointly nonsense — all of them produce text that looks plausible in a diff and does not compile.
// Parsing before writing turns every one of those into a refusal instead of a corrupted file.
//
// The check is syntax only, deliberately. A fix that leaves the file parseable but ill-typed is a
// bad fix and will be caught by the types phase on the same run, in the working tree, before
// commit. A fix that leaves the file unparseable destroys the ability of every later phase to say
// anything at all, which is why it is the one thing that must never reach disk.
//
// Measured: an unbalanced brace reports TS1005, a stray paren TS1005, a truncated statement
// TS1109, an unterminated string TS1002, and a line of garbage six diagnostics. Clean source and
// an empty file report none. The guard has been shown to fire, which is the only way to know it is
// a guard and not a decoration.
func Parses(fileName string, text string) (bool, string) {
	sourceFile := parseText(fileName, text)
	if sourceFile == nil {
		return false, "the parser produced nothing"
	}

	diagnostics := sourceFile.Diagnostics()
	if len(diagnostics) == 0 {
		return true, ""
	}

	first := diagnostics[0]
	reason := fmt.Sprintf("TS%d: %s", first.Code(), first.Message())
	if len(diagnostics) > 1 {
		reason = fmt.Sprintf("%s (and %d more)", reason, len(diagnostics)-1)
	}
	return false, reason
}

// parseText parses source text under a rooted, normalized file name.
//
// typescript-go stores the file name as an identity and panics on a relative path, so the name is
// rooted here rather than at each call site. The script kind comes from the extension because .tsx
// and .ts disagree about whether a `<T>` is a type argument or an element, and parsing one as the
// other invents syntax errors in a file that was fine.
func parseText(fileName string, text string) *ast.SourceFile {
	scriptKind := core.ScriptKindTS
	switch {
	case strings.HasSuffix(fileName, ".tsx"):
		scriptKind = core.ScriptKindTSX
	case strings.HasSuffix(fileName, ".jsx"):
		scriptKind = core.ScriptKindJSX
	case strings.HasSuffix(fileName, ".js"), strings.HasSuffix(fileName, ".mjs"), strings.HasSuffix(fileName, ".cjs"):
		scriptKind = core.ScriptKindJS
	}

	rooted := fileName
	if !tspath.IsRootedDiskPath(rooted) {
		rooted = "/" + strings.TrimPrefix(rooted, "/")
	}
	rooted = tspath.NormalizePath(rooted)

	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: rooted,
		Path:     tspath.Path(rooted),
	}, text, scriptKind)
}

// writeAtomically replaces a file's contents so that no reader ever observes a partial write.
//
// Write the new text to a temp file in the same directory, then rename over the target. Rename
// within a filesystem is atomic, so a concurrent reader sees the old bytes or the new bytes and
// never a truncated file. The same directory matters: a temp file in the system temp directory can
// be on a different filesystem, where rename degrades to copy-and-delete and the atomicity is
// silently lost.
//
// Five agents work in this tree at once, and a half-written file is worse than an unfixed one — an
// unfixed file still compiles. The failure this prevents is not theoretical here: a half-written
// file failing to build and then healing two minutes later was observed in this repo tonight.
//
// The original file's permission bits are preserved, because a fresh temp file is created at 0600
// and renaming it over a source file would quietly change its mode.
func writeAtomically(fileName string, text string) error {
	directory := filepath.Dir(fileName)

	mode := os.FileMode(0o644)
	if info, err := os.Stat(fileName); err == nil {
		mode = info.Mode().Perm()
	}

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(fileName)+".verify-*")
	if err != nil {
		return fmt.Errorf("creating a temp file beside %s: %w", fileName, err)
	}
	temporaryName := temporary.Name()

	// Any failure from here leaves a temp file behind unless it is removed, and a stale dotfile in
	// a source directory is its own small mess.
	cleanup := func() {
		temporary.Close()
		os.Remove(temporaryName)
	}

	if _, err := temporary.WriteString(text); err != nil {
		cleanup()
		return fmt.Errorf("writing %s: %w", temporaryName, err)
	}

	// Sync before rename. Without it the rename can be durable while the contents are not, which on
	// a crash produces a file that exists, has the right name, and holds zeros.
	//
	// This is the one guard in this package with no fixture behind it, and it is called out rather
	// than left quiet. Removing this call passes every test here, because the failure it prevents
	// needs the machine to lose power between the rename and the writeback — which no in-process
	// test can induce. Every other guard was verified by deliberately breaking it and watching a
	// fixture go red; this one rests on the argument alone. Treat it accordingly: it is load-bearing
	// and unverified, so do not remove it because the tests stay green.
	if err := temporary.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("syncing %s: %w", temporaryName, err)
	}
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryName)
		return fmt.Errorf("closing %s: %w", temporaryName, err)
	}
	if err := os.Chmod(temporaryName, mode); err != nil {
		os.Remove(temporaryName)
		return fmt.Errorf("setting the mode on %s: %w", temporaryName, err)
	}
	if err := os.Rename(temporaryName, fileName); err != nil {
		os.Remove(temporaryName)
		return fmt.Errorf("renaming %s over %s: %w", temporaryName, fileName, err)
	}

	return nil
}
