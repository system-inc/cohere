package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/native"
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/format/printing"
)

// The formatters --format-engine chooses between.
const (
	formatEnginePrettier = "prettier"
	formatEngineNative   = "native"
)

// formatEngine formats one file's text, or says it could not.
//
// This is the narrowest possible surface onto whichever formatter the pipeline runs, and it is an
// interface rather than a direct call for a reason that is about this tree rather than about
// abstraction taste: the engine that will implement it does not exist in-tree yet. Naming the shape
// lets the seam, the outcome mapping, and every fixture around it be written and tested now, and
// lets the real engine drop in as one line when it lands. The alternative was inferring the
// concrete package's signature from a message, which is exactly what produced `internal/differential`.
//
// Three outcomes, which is what @system_cohere_format reports and fewer than the four the native
// formatter reports:
//
//   - formatted text, no error   the transform's result
//   - an error                   the formatter could not produce output, whether because the source
//     did not parse or because the formatter itself failed
//   - Handles returning false    this engine does not format that file type at all
//
// The engine never signals "nothing to do" by returning its input unchanged. That is
// indistinguishable from "already correctly formatted", so a formatter that silently handled
// nothing would report a whole tree as clean. Same class as the `\p{...}` bug that returned false
// for every character without throwing: the dangerous failures do not announce themselves.
type formatEngine interface {
	// Format returns the formatted text, or an error.
	Format(fileName string, text string) (string, error)

	// Handles reports whether this engine formats a file's type at all.
	//
	// Separate from Format rather than folded into it as an error, because the caller needs the
	// answer before deciding whether a file belongs in the candidate set, and asking by attempting a
	// format would mean running the formatter to learn it should not have.
	Handles(fileName string) bool

	// Enumerate walks a project root and returns every file the engine would format, with an account
	// of what the walk removed at each step.
	//
	// It belongs on the engine rather than in the pipeline because it is extension and ignore-file
	// knowledge, the same knowledge Handles already encodes. The pipeline decides which of those
	// files are in scope; the engine decides which files are formattable at all.
	Enumerate(root string, structureIgnorePath string) (prettier.Enumeration, error)
}

// formatTransform adapts a format engine to the edit engine's whole-text transform.
//
// The edit engine owns what lands on disk, so formatting arrives as a transform rather than writing
// on its own. Two components writing the same file in one run is precisely the failure the edit
// engine exists to prevent, and building that into the pipeline that contains it would be a poor
// joke.
//
// It runs after fixes have converged, which is not arbitrary: fixes are semantic and change what the
// correct formatting is, so formatting first would leave every later fix mis-formatted and want a
// re-format. Formatting is total and idempotent, so running it last converges in one shot regardless
// of what the fixes did.
//
// The whole job here is keeping outcomes distinct that a careless adapter would collapse. A skip and
// a failure mean opposite things to a reader: one says the formatter chose not to look, the other
// says it looked and broke. Collapsing either into "returned the text unchanged" makes a formatter
// that never ran indistinguishable from a tree that was already correct, which is the ambiguity the
// coverage line exists to destroy.
func formatTransform(engine formatEngine) edit.Transform {
	return func(fileName string, text string) (string, error) {
		if engine == nil {
			return "", fmt.Errorf("%w: no formatter is configured", edit.ErrSkipped)
		}

		if !engine.Handles(fileName) {
			return "", fmt.Errorf("%w: %s is not a file type the formatter handles", edit.ErrSkipped, extensionOf(fileName))
		}

		formatted, err := engine.Format(fileName, text)
		if err != nil {
			// A failure is reported as a failure rather than downgraded to a skip. The edit engine
			// keeps the fixes that already converged, so a formatter falling over on one file does not
			// cost the tree its correctness fixes, and the refusal is counted rather than swallowed.
			//
			// The exception is a file that does not parse, which the engine may report as an error and
			// which is genuinely a skip: the edit engine already refuses to fix a file in that state,
			// and the types phase reports it in a form a reader can act on. A second complaint from the
			// formatter adds noise rather than information.
			if isUnparseable(err) {
				return "", fmt.Errorf("%w: the file does not parse, so there is nothing to format", edit.ErrSkipped)
			}
			return "", err
		}

		return formatted, nil
	}
}

// isUnparseable reports whether a format error was caused by source that does not parse.
//
// Matched on the message because the engine is behind an interface and its error types are its own.
// A false negative here costs a refusal where a skip was meant, which is the safe direction: it is
// reported and counted rather than hidden.
func isUnparseable(err error) bool {
	// The native printers mark their parse failures, so their wording never has to be guessed.
	if printing.IsSyntax(err) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"does not parse", "parse error", "syntaxerror", "unexpected token"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// extensionOf names a file's type for a skip reason, so the summary groups skips by what was
// skipped rather than listing one line per file.
func extensionOf(fileName string) string {
	lastDot := strings.LastIndex(fileName, ".")
	lastSlash := strings.LastIndexAny(fileName, "/\\")
	if lastDot <= lastSlash || lastDot < 0 {
		return "a file with no extension"
	}
	return fileName[lastDot:]
}

// configuredFormatter returns the formatter the pipeline runs, or nil when formatting was not asked
// for.
//
// A nil engine skips with "no formatter is configured" and that reaches the coverage line, so a run
// with no formatter reports as a run with no formatter rather than as a perfectly formatted tree.
//
// engineName picks cohere's native printers, the default, or the goja Prettier fork they replace,
// which stays only as the differential's oracle until it leaves the binary. The native printers were
// made the default when they matched the fork on every file of ahra, www-phi-health and
// api-phi-health, and when a whole-tree run of each engine over the same ahra snapshot wrote the
// same bytes. Both resolve each file's options from its own directory, so choosing one changes the
// printers and nothing else.
func configuredFormatter(enabled bool, engineName string) (formatEngine, error) {
	if !enabled {
		return nil, nil
	}

	// Resolving, not one engine built from DefaultOptions: each file formats with the config Prettier
	// would resolve for it. DefaultOptions is ahra's block, and running it over api-phi-health rewrote
	// every multi-line JSX opening tag, because that repository sets bracketSameLine.
	workingDirectory, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("finding where to resolve the Prettier config from: %w", err)
	}

	switch engineName {
	case formatEngineNative:
		engine, err := native.NewResolving(workingDirectory)
		if err != nil {
			return nil, fmt.Errorf("loading the native formatter: %w", err)
		}
		return engine, nil
	case formatEnginePrettier:
	default:
		return nil, fmt.Errorf("--format-engine %q is not a formatter: use %s or %s", engineName, formatEnginePrettier, formatEngineNative)
	}

	engine, err := prettier.NewResolving(workingDirectory)
	if err != nil {
		// Not a nil engine. Nil already means "nobody asked for a formatter", and the coverage line
		// reports that as a deliberate absence. An engine that was asked for and could not load its
		// bundles is a different fact, and collapsing the two would print a failure as a choice.
		return nil, fmt.Errorf("loading the prettier engine: %w", err)
	}
	return engine, nil
}
