package main

import (
	"fmt"
	"strings"

	"github.com/system-inc/verify/internal/fix"
	"github.com/system-inc/verify/internal/prettier"
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
// Three outcomes, which is what @system_verify_format reports and fewer than the four the native
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
func formatTransform(engine formatEngine) fix.Transform {
	return func(fileName string, text string) (string, error) {
		if engine == nil {
			return "", fmt.Errorf("%w: no formatter is configured", fix.ErrSkipped)
		}

		if !engine.Handles(fileName) {
			return "", fmt.Errorf("%w: %s is not a file type the formatter handles", fix.ErrSkipped, extensionOf(fileName))
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
				return "", fmt.Errorf("%w: the file does not parse, so there is nothing to format", fix.ErrSkipped)
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

// configuredFormatter returns the formatter the pipeline runs, or nil when none is available.
//
// It is nil today, deliberately and visibly. The engine is @system_verify_format's goja Prettier
// fork, which is not in this module yet; when it lands, this function returns it and nothing else
// in the pipeline changes.
//
// Returning nil rather than quietly wiring the native formatter is the whole point. `formatdiff`
// exists in-tree and would compile, and it is the wrong engine: FormatCodeSettings has no
// printWidth, so it has no line-breaking engine at all, and 21.8% of tracked files diverge from our
// Prettier after every settings-reachable fix. A bare `verify` running it would reformat about a
// fifth of the tree away from what the existing gate produces, which is worse than not formatting.
//
// A nil engine skips with "no formatter is configured" and that reaches the coverage line, so a run
// with no formatter reports as a run with no formatter rather than as a perfectly formatted tree.
// The absence is stated on every run instead of being discovered later.
//
// The engine has landed, so this now returns it, and the `enabled` flag is what stays off. Wiring
// and enabling are separate acts: the seam is proven, and whether a bare `verify` should rewrite
// files is a question about corpus agreement rather than about plumbing.
func configuredFormatter(enabled bool) (formatEngine, error) {
	if !enabled {
		return nil, nil
	}

	engine, err := prettier.New(prettier.DefaultOptions())
	if err != nil {
		// Not a nil engine. Nil already means "nobody asked for a formatter", and the coverage line
		// reports that as a deliberate absence. An engine that was asked for and could not load its
		// bundles is a different fact, and collapsing the two would print a failure as a choice.
		return nil, fmt.Errorf("loading the prettier engine: %w", err)
	}
	return engine, nil
}
