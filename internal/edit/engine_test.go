package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// proposeOnce returns a fixed set of proposals on the first pass and nothing afterwards, which is
// what a well-behaved rule looks like: it sees the violation, proposes, and the violation is gone.
func proposeOnce(proposals ...Proposal) Propose {
	handedOut := false
	return func(fileName string, text string) ([]Proposal, error) {
		if handedOut {
			return nil, nil
		}
		handedOut = true
		return proposals, nil
	}
}

// proposeWhileContains re-derives a proposal from the current text every pass, which is what a real
// rule does: it is re-run against the rewritten file and reports whatever is still wrong. Passing
// text through means the offsets are always measured against the bytes they will be applied to.
func proposeWhileContains(needle string, replacement string, ruleName string) Propose {
	return func(fileName string, text string) ([]Proposal, error) {
		index := strings.Index(text, needle)
		if index < 0 {
			return nil, nil
		}
		return []Proposal{{
			RuleName: ruleName,
			Fix:      rule.Fix{Range: core.NewTextRange(index, index+len(needle)), Text: replacement},
		}}, nil
	}
}

// A fix that produces invalid syntax must be refused and the text left exactly as it was. This is
// the guard the whole package rests on.
func TestFixProducingInvalidSyntaxIsRefused(t *testing.T) {
	source := "function alpha() { return 1; }\n"

	// Delete the closing brace: individually a legal-looking range replacement, and the result does
	// not parse.
	closingBrace := strings.LastIndex(source, "}")
	propose := proposeOnce(proposal("brace-eater", closingBrace, closingBrace+1, ""))

	result, err := FixText("broken.ts", source, propose, DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Changed {
		t.Fatalf("the engine reported a change despite the parse failure")
	}
	if result.Text != source {
		t.Fatalf("the text was modified despite the refusal:\n  want %q\n  got  %q", source, result.Text)
	}
	if len(result.Applied) != 0 {
		t.Fatalf("expected nothing applied, got %d", len(result.Applied))
	}
	if len(result.Rejected) != 1 {
		t.Fatalf("expected 1 rejection, got %d: %+v", len(result.Rejected), result.Rejected)
	}
	if !strings.HasPrefix(result.Rejected[0].Reason, ReasonParseFailure) {
		t.Fatalf("expected a parse-failure reason, got %q", result.Rejected[0].Reason)
	}
	// The compiler's own message must ride along, or a reader cannot act on the refusal.
	if !strings.Contains(result.Rejected[0].Reason, "TS") {
		t.Fatalf("the refusal reason carries no compiler diagnostic: %q", result.Rejected[0].Reason)
	}
}

// A pass is discarded whole. When one fix in a pass breaks the parse, the fixes that were
// individually fine must not land either — writing a combination no rule proposed and nothing
// verified is a worse guarantee than leaving the file alone.
func TestAParseFailureDiscardsTheWholePass(t *testing.T) {
	source := "const alpha = 1;\nfunction beta() { return 2; }\n"

	closingBrace := strings.LastIndex(source, "}")
	goodStart := strings.Index(source, "alpha")

	propose := proposeOnce(
		proposal("good-rule", goodStart, goodStart+5, "renamed"),
		proposal("brace-eater", closingBrace, closingBrace+1, ""),
	)

	result, err := FixText("mixed.ts", source, propose, DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Text != source {
		t.Fatalf("a fix landed from a discarded pass:\n  want %q\n  got  %q", source, result.Text)
	}
	if len(result.Rejected) != 2 {
		t.Fatalf("expected both fixes refused, got %d: %+v", len(result.Rejected), result.Rejected)
	}
}

// A file that needs several passes must converge and report how many it took. A fix exposing a new
// violation is the normal case, and one pass would leave the file half-fixed while reporting
// success.
func TestConvergenceTakesSeveralPasses(t *testing.T) {
	// Each pass replaces one "old" with "new". Three of them means three productive passes plus the
	// pass that finds nothing left.
	source := "const a = 'old'; const b = 'old'; const c = 'old';\n"

	result, err := FixText("converge.ts", source, proposeWhileContains("'old'", "'new'", "renamer"), DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Converged {
		t.Fatalf("expected convergence, got %+v", result)
	}
	if strings.Contains(result.Text, "old") {
		t.Fatalf("did not converge to a fixpoint: %q", result.Text)
	}
	if result.Passes != 4 {
		t.Fatalf("expected 4 passes (3 productive plus the quiet one), got %d", result.Passes)
	}
	if len(result.Applied) != 3 {
		t.Fatalf("expected 3 fixes applied, got %d", len(result.Applied))
	}
}

// Two rules that undo each other must terminate on the pass budget rather than spin forever, and
// must say so. A tool whose premise is a quarter-second run cannot have an unbounded loop in it.
func TestTwoRulesFightingTerminateOnTheBudget(t *testing.T) {
	// One rule rewrites ping to pong; the other rewrites pong back to ping. Neither is wrong on its
	// own and together they never settle.
	propose := func(fileName string, text string) ([]Proposal, error) {
		if index := strings.Index(text, "ping"); index >= 0 {
			return []Proposal{{
				RuleName: "prefer-pong",
				Fix:      rule.Fix{Range: core.NewTextRange(index, index+4), Text: "pong"},
			}}, nil
		}
		if index := strings.Index(text, "pong"); index >= 0 {
			return []Proposal{{
				RuleName: "prefer-ping",
				Fix:      rule.Fix{Range: core.NewTextRange(index, index+4), Text: "ping"},
			}}, nil
		}
		return nil, nil
	}

	result, err := FixText("fight.ts", "const a = 'ping';\n", propose, 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Converged {
		t.Fatalf("two rules fighting reported convergence")
	}
	if result.Passes != 6 {
		t.Fatalf("expected the loop to stop at the 6-pass budget, got %d", result.Passes)
	}
	// The text is whatever the last accepted pass produced, and it must be a valid file.
	if parses, reason := Parses("fight.ts", result.Text); !parses {
		t.Fatalf("the non-converged text does not parse: %s", reason)
	}
	// The ceiling is reported rather than swallowed.
	found := false
	for _, rejection := range result.Rejected {
		if strings.HasPrefix(rejection.Reason, ReasonPassesReached) {
			found = true
		}
	}
	if !found {
		t.Fatalf("hitting the pass ceiling was not reported: %+v", result.Rejected)
	}
}

// A file that is already broken must be refused up front. "The result parses" guarantees nothing if
// the input did not, and pretending otherwise would let this package take the blame for damage it
// did not do — or worse, hide damage it did.
func TestAlreadyBrokenFileIsRefusedUpFront(t *testing.T) {
	_, err := FixText("broken.ts", "function alpha() { return 1;\n", proposeOnce(), DefaultMaxPasses)
	if err == nil {
		t.Fatalf("expected an error for a file that does not parse to begin with")
	}
	if !strings.Contains(err.Error(), "does not parse") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The write must be atomic and must preserve the file's mode. A fresh temp file is created at 0600,
// and renaming it over a source file would silently change permissions.
//
// The fixture mode is deliberately 0644 rather than 0600. An earlier version used 0600 and a mutant
// that chmods unconditionally to 0600 passed it — the assertion was true for the wrong reason,
// which is the exact shape of a check that cannot fail. Picking a mode a temp file never has by
// default is what makes the assertion mean something.
func TestFixFileWritesAtomicallyAndKeepsMode(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "target.ts")
	source := "const a = 'old';\n"

	const sourceMode = os.FileMode(0o644)
	if err := os.WriteFile(fileName, []byte(source), sourceMode); err != nil {
		t.Fatal(err)
	}
	// os.WriteFile applies the process umask, so the file on disk may not be what was asked for.
	// Setting it explicitly means the assertion below compares against a mode that is really there.
	if err := os.Chmod(fileName, sourceMode); err != nil {
		t.Fatal(err)
	}

	result, err := FixFile(fileName, proposeWhileContains("'old'", "'new'", "renamer"), DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Changed {
		t.Fatalf("expected the file to change")
	}

	written, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "const a = 'new';\n" {
		t.Fatalf("wrong contents on disk: %q", written)
	}

	info, err := os.Stat(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != sourceMode {
		t.Fatalf("mode changed from %v to %v", sourceMode, info.Mode().Perm())
	}

	// No temp file may survive a successful write.
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("expected only the target file, found %v", names)
	}
}

// A file with nothing to apply must not be written at all. Rewriting identical bytes updates the
// modification time, which invalidates every downstream cache keyed on it and makes a run that
// changed nothing look like a run that changed everything.
func TestFixFileDoesNotTouchAnUnchangedFile(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "clean.ts")

	if err := os.WriteFile(fileName, []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(fileName)
	if err != nil {
		t.Fatal(err)
	}

	result, err := FixFile(fileName, proposeOnce(), DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Changed {
		t.Fatalf("reported a change on a file with no proposals")
	}

	after, err := os.Stat(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("an unchanged file was rewritten (mtime moved)")
	}
}

// A refused fix must leave the file on disk untouched, not merely leave the in-memory text alone.
func TestFixFileLeavesTheFileUntouchedOnRefusal(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "guarded.ts")
	source := "function alpha() { return 1; }\n"

	if err := os.WriteFile(fileName, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	closingBrace := strings.LastIndex(source, "}")
	result, err := FixFile(fileName, proposeOnce(proposal("brace-eater", closingBrace, closingBrace+1, "")), DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Changed {
		t.Fatalf("reported a change despite the refusal")
	}

	onDisk, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != source {
		t.Fatalf("the file on disk was modified:\n  want %q\n  got  %q", source, onDisk)
	}
}

// A missing file is an error, not an empty string. An empty string parses, applies nothing, and
// reports success, which is the shape of every silent-green failure this project has recorded.
func TestMissingFileIsAnError(t *testing.T) {
	_, err := FixFile(filepath.Join(t.TempDir(), "absent.ts"), proposeOnce(), DefaultMaxPasses)
	if err == nil {
		t.Fatalf("expected an error for a file that does not exist")
	}
}

// The summary must distinguish a run that changed forty files from one that changed none, and must
// say why fixes were refused rather than only how many.
func TestSummaryReportsPopulationAndReasons(t *testing.T) {
	summary := Summarize([]FileResult{
		{FileName: "a.ts", Changed: true, Passes: 1, Applied: []Proposal{{RuleName: "r"}}},
		{FileName: "b.ts", Changed: true, Passes: 3, Applied: []Proposal{{RuleName: "r"}, {RuleName: "r"}}},
		{FileName: "c.ts", Changed: false, Passes: 1, Rejected: []Rejection{
			{Reason: ReasonOverlap},
			{Reason: ReasonParseFailure + " (TS1005: '}' expected.)"},
		}},
		{FileName: "d.ts", Changed: false, Passes: 1},
	})

	if summary.FilesConsidered != 4 {
		t.Fatalf("expected 4 files considered, got %d", summary.FilesConsidered)
	}
	if summary.FilesChanged != 2 {
		t.Fatalf("expected 2 files changed, got %d", summary.FilesChanged)
	}
	if summary.FixesApplied != 3 {
		t.Fatalf("expected 3 fixes applied, got %d", summary.FixesApplied)
	}
	if summary.FixesRefused != 2 {
		t.Fatalf("expected 2 refusals, got %d", summary.FixesRefused)
	}
	// A parse-failure reason carries the compiler's message, so the tally must collapse it to its
	// category rather than counting four hundred distinct singletons.
	if summary.RefusalsByReason[ReasonParseFailure] != 1 {
		t.Fatalf("parse failures were not collapsed to their category: %+v", summary.RefusalsByReason)
	}
	if summary.RefusalsByReason[ReasonOverlap] != 1 {
		t.Fatalf("expected 1 overlap refusal, got %+v", summary.RefusalsByReason)
	}
	if len(summary.FilesRefused) != 1 || summary.FilesRefused[0] != "c.ts" {
		t.Fatalf("expected c.ts named as refused, got %v", summary.FilesRefused)
	}

	line := summary.String()
	for _, want := range []string{"2 of 4 files rewritten", "3 fixes applied", "2 refused", "up to 3 passes"} {
		if !strings.Contains(line, want) {
			t.Fatalf("summary line is missing %q: %s", want, line)
		}
	}
}

// A file the engine could not process must be counted as failed, not as not-converged.
//
// The two send a reader somewhere different: not-converged points at two rules arguing, while a
// failure means the file never entered the loop. This was a real defect in the CLI driver — a file
// that did not parse before any fix was applied reported as "1 files did not converge", a true
// number under the wrong heading, which is the shape of an accurate answer to a question nobody
// asked.
func TestAFailedFileIsNotReportedAsNotConverged(t *testing.T) {
	summary := Summarize([]FileResult{
		{FileName: "unreadable.ts", Failed: true},
		{FileName: "stubborn.ts", Passes: 10, Converged: false, Changed: true},
	})

	if len(summary.FilesFailed) != 1 || summary.FilesFailed[0] != "unreadable.ts" {
		t.Fatalf("expected unreadable.ts counted as failed, got %v", summary.FilesFailed)
	}
	if len(summary.FilesNotConverged) != 1 || summary.FilesNotConverged[0] != "stubborn.ts" {
		t.Fatalf("expected only stubborn.ts counted as not converged, got %v", summary.FilesNotConverged)
	}

	line := summary.String()
	if !strings.Contains(line, "1 files could not be processed") {
		t.Fatalf("the summary hides the failure: %s", line)
	}
	if !strings.Contains(line, "1 files did not converge") {
		t.Fatalf("the summary lost the non-convergence: %s", line)
	}
}

// A run that changed nothing must not print the same line as a run that changed everything. This is
// the ambiguity that let the gate cohere replaces print green over zero files for days.
func TestSummaryDistinguishesAnEmptyRun(t *testing.T) {
	empty := Summarize(nil).String()
	busy := Summarize([]FileResult{
		{FileName: "a.ts", Changed: true, Passes: 1, Applied: []Proposal{{RuleName: "r"}}},
	}).String()

	if empty == busy {
		t.Fatalf("an empty run and a busy run print the same line: %q", empty)
	}
	if !strings.Contains(empty, "0 of 0 files") {
		t.Fatalf("an empty run does not state its population: %q", empty)
	}
}

// TSX must be parsed as TSX. A `<T>` is a type assertion in .ts and an element in .tsx, so parsing
// one as the other invents syntax errors in a file that was fine — and the refusal guard would then
// refuse every correct fix in every .tsx file, silently, forever.
func TestTsxIsParsedAsTsx(t *testing.T) {
	element := "const view = <div className=\"a\">text</div>;\n"

	if parses, reason := Parses("component.tsx", element); !parses {
		t.Fatalf("valid TSX was rejected: %s", reason)
	}
	if parses, _ := Parses("component.ts", element); parses {
		t.Fatalf("the .ts and .tsx parses are identical, so the script kind is not being honored")
	}
}
