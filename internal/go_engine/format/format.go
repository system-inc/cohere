// Package format is the Go engine's fix phase: gofmt over every file in scope, run before the load so the load
// reads the formatted tree. A run either rewrites what gofmt would change or, for `--no-fix`, writes nothing
// and says where each file would change, which the engine reports as one finding per file at that line. A run that
// rewrites may also wrap each file's long lines after gofmt, through Options.Wrap, keeping the wrap only when it is
// provably the same program and gofmt leaves it as it is.
//
// A formatter that formatted nothing must never read like one that found everything already correct, so
// every file a run is handed comes back accounted for: already formatted, changed, not formatted with its
// reason, or skipped as generated.
package format

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	goformat "go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"

	"github.com/system-inc/cohere/internal/line_wrap"
)

// Options says what a run does with the files gofmt would change.
type Options struct {
	// Write rewrites each file gofmt would change. Unset, the run is the check `--no-fix` asks for: it reports
	// every file gofmt would change and writes nothing, so the zero value never touches a project.
	Write bool

	// SingleThreaded formats one file at a time, for `--single-threaded`.
	SingleThreaded bool

	// Known is the verdicts of files gofmt had nothing to do to in an earlier run, read in place of formatting a
	// file whose bytes are the same, and given what this run learns. Nil formats every file.
	Known *KnownFiles

	// Wrap, when set, wraps the long lines of each file gofmt formats or finds formatted, under Write only: a check
	// leaves long lines to the lint phase, which reports them as the rule's findings. It is handed gofmt's output
	// and says what it would write instead. The run keeps that only when it is the same program as gofmt's output
	// (line_wrap.Equivalent) and gofmt leaves it as it is; otherwise it writes gofmt's output and names the file in
	// Result.WrapDeclined, and the long lines stay findings.
	Wrap func(formatted []byte) Wrapping
}

// Wrapping is what Options.Wrap would make of one file's formatted text.
type Wrapping struct {
	// Output is the wrapped text, nil when the wrap leaves the file as gofmt has it.
	Output []byte

	// Lines is how many long lines Output brings within the limit, at least one whenever Output is set.
	Lines int

	// Declined is why the wrap gave up on a file it set out to change, a check of its own that failed. Empty when
	// it had nothing to do or Output is set.
	Declined string
}

// ChangedFile is a file gofmt changed, or would change under a check, or whose long lines a write wrapped.
type ChangedFile struct {
	File string

	// Formatted is whether gofmt changed the file, false for one only the wrap changed.
	Formatted bool

	// WrappedLines is how many long lines the wrap brought within the limit, none when the wrap left the file alone.
	WrappedLines int

	// Written is whether the run rewrote the file, which only a run with Options.Write does.
	Written bool

	// FirstDifferingLine is the 1-based line of the original file where gofmt's output first differs, the line
	// a check's finding points at.
	FirstDifferingLine int
}

// WrapDeclinedFile is a file whose wrap the run did not keep, and why. The file holds gofmt's output.
type WrapDeclinedFile struct {
	File   string
	Reason string
}

// NotFormattedFile is a file the run could not format, and why: it could not be read, it does not parse, or
// the formatted result could not be written. The file is left as it was.
type NotFormattedFile struct {
	File   string
	Reason string
}

// Result is one run. Each list keeps the order the files were given in, so the same input always reads the same
// whatever order the files finished in.
type Result struct {
	// FilesConsidered is every file the run was handed. Each is in exactly one of Changed, NotFormatted and
	// Generated, or none of them when gofmt would leave it as it is.
	FilesConsidered int

	Changed      []ChangedFile
	NotFormatted []NotFormattedFile

	// WrapDeclined is the files whose wrap the run did not keep. Each is also in Changed when gofmt changed it.
	WrapDeclined []WrapDeclinedFile

	// Generated is the files skipped as generated, by the rule go/ast.IsGenerated reads: a comment line
	// `// Code generated ... DO NOT EDIT.` before the package clause. Their output is their generator's, so
	// they are neither formatted nor reported.
	Generated []string

	// Known counts the files answered from Options.Known rather than formatted, each one gofmt left as it was, or
	// skipped as generated, when it last saw the same bytes.
	Known int
}

// outcome is what a run did with one file: at most one of changed, notFormatted and generated is set, and none for
// a file the run would leave as it is. A declined wrap may come with a change gofmt made.
type outcome struct {
	changed      *ChangedFile
	notFormatted *NotFormattedFile
	generated    bool
	wrapDeclined *WrapDeclinedFile

	// known is a file answered from Options.Known.
	known bool
}

// Run formats files, which are absolute paths, as many at once as there are cores unless
// options.SingleThreaded, since each file is formatted on its own.
func Run(files []string, options Options) Result {
	outcomes := make([]outcome, len(files))
	if options.SingleThreaded {
		for index, file := range files {
			outcomes[index] = formatFile(file, options)
		}
	} else {
		slots := make(chan struct{}, runtime.GOMAXPROCS(0))
		var group sync.WaitGroup
		for index, file := range files {
			slots <- struct{}{}
			group.Go(func() {
				defer func() { <-slots }()
				outcomes[index] = formatFile(file, options)
			})
		}
		group.Wait()
	}

	result := Result{FilesConsidered: len(files)}
	for index, file := range files {
		if outcomes[index].known {
			result.Known++
		}
		if outcomes[index].wrapDeclined != nil {
			result.WrapDeclined = append(result.WrapDeclined, *outcomes[index].wrapDeclined)
		}
		switch {
		case outcomes[index].changed != nil:
			result.Changed = append(result.Changed, *outcomes[index].changed)
		case outcomes[index].notFormatted != nil:
			result.NotFormatted = append(result.NotFormatted, *outcomes[index].notFormatted)
		case outcomes[index].generated:
			result.Generated = append(result.Generated, file)
		}
	}
	return result
}

// formatFile formats one file and wraps its long lines when options asks, writing the result under options.Write,
// unless options.Known says what the run made of the same bytes before.
func formatFile(file string, options Options) outcome {
	original, readError := os.ReadFile(file)
	if readError != nil {
		return notFormatted(file, fmt.Sprintf("it cannot be read: %v", readError))
	}
	wraps := options.Write && options.Wrap != nil
	var digest [sha256.Size]byte
	if options.Known != nil {
		digest = sha256.Sum256(original)
		if verdict, held := options.Known.verdict(digest, wraps); held {
			return outcome{generated: verdict == VerdictGenerated, known: true}
		}
	}

	// The package clause and the comments above it are all the generated rule reads. A parse that stops there
	// also refuses what gofmt refuses and format.Source does not: a file with no package clause, which
	// format.Source would format as a fragment of declarations.
	headerMode := parser.PackageClauseOnly | parser.ParseComments
	header, headerError := parser.ParseFile(token.NewFileSet(), "", original, headerMode)
	if headerError != nil {
		return notFormatted(file, fmt.Sprintf("it does not parse: %v", headerError))
	}
	if ast.IsGenerated(header) {
		if options.Known != nil {
			options.Known.learn(digest, VerdictGenerated)
		}
		return outcome{generated: true}
	}

	formatted, formatError := goformat.Source(original)
	if formatError != nil {
		return notFormatted(file, formatFailureReason(formatError))
	}
	result := outcome{}
	output := formatted
	wrappedLines := 0
	if wraps {
		var declined string
		output, wrappedLines, declined = keptWrap(formatted, options.Wrap(formatted))
		if declined != "" {
			result.wrapDeclined = &WrapDeclinedFile{File: file, Reason: declined}
		}
	}
	if bytes.Equal(output, original) {
		// A declined wrap is never known, so the next run tries it again and says again why it was not kept.
		if options.Known != nil && result.wrapDeclined == nil {
			verdict := VerdictUnchanged
			if wraps {
				verdict = VerdictWrapUnchanged
			}
			options.Known.learn(digest, verdict)
		}
		return result
	}
	changed := &ChangedFile{
		File:               file,
		Formatted:          !bytes.Equal(formatted, original),
		WrappedLines:       wrappedLines,
		FirstDifferingLine: firstDifferingLine(original, output),
	}
	if options.Write {
		if writeError := writeAtomically(file, output); writeError != nil {
			return notFormatted(file, fmt.Sprintf("it cannot be written: %v", writeError))
		}
		changed.Written = true
	}
	result.changed = changed
	return result
}

// keptWrap is what a run writes of a wrapping of formatted, gofmt's output: the wrap's text and the lines it brought
// within the limit when it is the same program and gofmt leaves it as it is, and otherwise formatted, with why the
// wrap was not kept unless it had nothing to do.
func keptWrap(formatted []byte, wrapping Wrapping) (output []byte, lines int, declined string) {
	if wrapping.Declined != "" {
		return formatted, 0, wrapping.Declined
	}
	if wrapping.Output == nil || bytes.Equal(wrapping.Output, formatted) {
		return formatted, 0, ""
	}
	if wrapping.Lines < 1 {
		return formatted, 0, "the wrap changed the file and brought no long line within the limit"
	}
	if equivalentError := line_wrap.Equivalent(formatted, wrapping.Output); equivalentError != nil {
		return formatted, 0, fmt.Sprintf("the wrap is not the same program: %v", equivalentError)
	}
	reformatted, reformatError := goformat.Source(wrapping.Output)
	if reformatError != nil {
		return formatted, 0, fmt.Sprintf("gofmt cannot format the wrap: %v", reformatError)
	}
	if !bytes.Equal(reformatted, wrapping.Output) {
		return formatted, 0, fmt.Sprintf("gofmt would change the wrap from line %d",
			firstDifferingLine(wrapping.Output, reformatted))
	}
	return wrapping.Output, wrapping.Lines, ""
}

// notFormatted is the outcome for a file left as it was, and why.
func notFormatted(file, reason string) outcome {
	return outcome{notFormatted: &NotFormattedFile{File: file, Reason: reason}}
}

// formatFailureReason says why format.Source refused a file whose package clause parsed: almost always a parse
// error, in gofmt's own words, and otherwise whatever the printer said.
func formatFailureReason(formatError error) string {
	var parseErrors scanner.ErrorList
	if errors.As(formatError, &parseErrors) {
		return fmt.Sprintf("it does not parse: %v", formatError)
	}
	return fmt.Sprintf("gofmt could not print it: %v", formatError)
}

// firstDifferingLine is the 1-based line of original where formatted first differs. Lines are compared with their
// newlines, so a last line missing its newline differs on that line, and a blank line gofmt drops is reported
// where it stands. The answer is never past original's last line, since a finding must point into the file.
func firstDifferingLine(original, formatted []byte) int {
	originalLines := slices.Collect(bytes.Lines(original))
	formattedLines := slices.Collect(bytes.Lines(formatted))
	for index, line := range originalLines {
		if index >= len(formattedLines) || !bytes.Equal(line, formattedLines[index]) {
			return index + 1
		}
	}
	// Every original line survived and gofmt only added after them, which it does not do to a file that parses.
	return max(1, len(originalLines))
}

// writeAtomically replaces file with contents through a temporary file beside it and a rename, so an interrupted
// run leaves the old file or the new one, never half of either. The temporary file takes the file's permissions
// before the rename makes it the file. A symbolic link is written through to its target, and stays a link.
//
// Nothing is synced to disk: the rename already guards against the run being stopped, and a sync per file
// would cost every run to guard against the machine losing power mid-write.
func writeAtomically(file string, contents []byte) error {
	target, resolveError := filepath.EvalSymlinks(file)
	if resolveError != nil {
		return fmt.Errorf("resolving %s: %w", file, resolveError)
	}
	information, statError := os.Stat(target)
	if statError != nil {
		return fmt.Errorf("reading the mode of %s: %w", target, statError)
	}
	temporary, createError := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".cohere-format-*")
	if createError != nil {
		return fmt.Errorf("creating a temporary file beside %s: %w", target, createError)
	}
	if fillError := fillTemporary(temporary, contents, information.Mode().Perm()); fillError != nil {
		// Ignored: the temporary file is ours and half written, and the error worth returning is the fill's.
		_ = os.Remove(temporary.Name())
		return fillError
	}
	if renameError := os.Rename(temporary.Name(), target); renameError != nil {
		// Ignored: the temporary file is ours, and the error worth returning is the rename's.
		_ = os.Remove(temporary.Name())
		return fmt.Errorf("renaming %s over %s: %w", temporary.Name(), target, renameError)
	}
	return nil
}

// fillTemporary writes contents to temporary, gives it permissions, and closes it, which it does whatever else
// failed.
func fillTemporary(temporary *os.File, contents []byte, permissions os.FileMode) error {
	_, writeError := temporary.Write(contents)
	chmodError := temporary.Chmod(permissions)
	closeError := temporary.Close()
	switch {
	case writeError != nil:
		return fmt.Errorf("writing %s: %w", temporary.Name(), writeError)
	case chmodError != nil:
		return fmt.Errorf("setting the mode of %s: %w", temporary.Name(), chmodError)
	case closeError != nil:
		return fmt.Errorf("closing %s: %w", temporary.Name(), closeError)
	}
	return nil
}
