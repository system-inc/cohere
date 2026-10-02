package edit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The transform runs after fixes converge, and it sees the fixed text rather than the original.
//
// The order is the whole point of the seam: fixes are semantic and change what the correct
// formatting is, so a transform that ran first would be formatting text that is about to change.
func TestTransformRunsAfterFixesAndSeesTheFixedText(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "ordered.ts")

	if err := os.WriteFile(directory+"/ordered.ts", []byte("const a = 'old';\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sawByTransform := ""
	transform := func(_ string, text string) (string, error) {
		sawByTransform = text
		return strings.ReplaceAll(text, "const", "export const"), nil
	}

	result, err := FixAndTransformFile(fileName, proposeWhileContains("'old'", "'new'", "renamer"), transform, DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sawByTransform != "const a = 'new';\n" {
		t.Fatalf("the transform saw unfixed text: %q", sawByTransform)
	}
	if !result.Transformed {
		t.Fatalf("the transform changed the text but was not reported as having done so")
	}

	onDisk, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "export const a = 'new';\n" {
		t.Fatalf("wrong contents on disk: %q", onDisk)
	}
}

// A transform whose output does not parse is refused, and the fixes that already converged still
// land. A broken formatter must not be able to block every correctness fix in the tree.
func TestATransformProducingInvalidSyntaxIsRefused(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "guarded.ts")

	if err := os.WriteFile(fileName, []byte("const a = 'old';\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	breaking := func(_ string, text string) (string, error) {
		return text + "function unclosed() {\n", nil
	}

	result, err := FixAndTransformFile(fileName, proposeWhileContains("'old'", "'new'", "renamer"), breaking, DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Transformed {
		t.Fatalf("an unparseable transform was reported as applied")
	}

	// The fix survives; only the transform is discarded.
	onDisk, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "const a = 'new';\n" {
		t.Fatalf("expected the fix to land and the transform to be refused, got %q", onDisk)
	}

	refused := false
	for _, rejection := range result.Rejected {
		if strings.HasPrefix(rejection.Reason, ReasonParseFailure) && rejection.Proposal.RuleName == transformRuleName {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("the transform refusal was not reported: %+v", result.Rejected)
	}
}

// A transform that errors is reported, and the fixes still land. The formatter being broken says
// nothing about whether the fixes were correct — they already converged and already passed the guard.
func TestATransformThatErrorsDoesNotBlockFixes(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "erroring.ts")

	if err := os.WriteFile(fileName, []byte("const a = 'old';\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	failing := func(_ string, _ string) (string, error) {
		return "", errors.New("the formatter fell over")
	}

	result, err := FixAndTransformFile(fileName, proposeWhileContains("'old'", "'new'", "renamer"), failing, DefaultMaxPasses)
	if err != nil {
		t.Fatalf("a transform error must not fail the whole file: %v", err)
	}

	onDisk, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "const a = 'new';\n" {
		t.Fatalf("the fix did not land when the transform failed: %q", onDisk)
	}

	reported := false
	for _, rejection := range result.Rejected {
		if strings.HasPrefix(rejection.Reason, ReasonTransformFailed) {
			reported = true
			if !strings.Contains(rejection.Reason, "fell over") {
				t.Fatalf("the refusal lost the underlying error: %q", rejection.Reason)
			}
		}
	}
	if !reported {
		t.Fatalf("a failing transform was not reported: %+v", result.Rejected)
	}
}

// A transform must run even when no fix landed. A file can be correctly written and badly
// formatted, and a formatter that only ran on files with findings would never touch most of a tree.
func TestTransformRunsWhenNoFixLanded(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "onlyformat.ts")

	if err := os.WriteFile(fileName, []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FixAndTransformFile(fileName, proposeOnce(), func(_ string, text string) (string, error) {
		return "export " + text, nil
	}, DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Transformed || !result.Changed {
		t.Fatalf("the transform did not run on a file with no fixes: %+v", result)
	}

	onDisk, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "export const a = 1;\n" {
		t.Fatalf("wrong contents: %q", onDisk)
	}
}

// A transform that returns the text unchanged is not a change, and the file must not be rewritten.
// Touching a file that needed nothing moves its modification time and invalidates every downstream
// cache keyed on it.
func TestAnIdentityTransformDoesNotTouchTheFile(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "already.ts")

	if err := os.WriteFile(fileName, []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(fileName)
	if err != nil {
		t.Fatal(err)
	}

	result, err := FixAndTransformFile(fileName, proposeOnce(), func(_ string, text string) (string, error) {
		return text, nil
	}, DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Changed || result.Transformed {
		t.Fatalf("an identity transform reported a change: %+v", result)
	}

	after, err := os.Stat(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("a file needing no transform was rewritten")
	}
}

// The transform must be idempotent: running it on its own output must produce the same bytes.
//
// This is the property the engine relies on and it is worth an explicit guard rather than an
// assumption. A formatter that alternates between two valid outputs would make every run rewrite
// every file, so a tree would never reach a steady state and every commit would carry churn nobody
// authored. Measuring against a reference implementation proves agreement with that implementation;
// it does not prove a second pass is a no-op, which is a different question.
//
// The control below is a deliberately non-idempotent transform, so this fixture has been shown to
// fail rather than only to pass.
func TestTransformIdempotenceIsCheckable(t *testing.T) {
	source := "const a = 1;\n"

	stable := func(_ string, text string) (string, error) {
		return strings.ReplaceAll(text, "const", "let"), nil
	}
	first, err := stable("f.ts", source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := stable("f.ts", first)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("expected the stable transform to be idempotent: %q then %q", first, second)
	}

	// The control: a transform that grows its output every pass. If this fixture cannot tell the
	// difference, it cannot check idempotence at all.
	unstable := func(_ string, text string) (string, error) {
		return "// pass\n" + text, nil
	}
	firstUnstable, err := unstable("f.ts", source)
	if err != nil {
		t.Fatal(err)
	}
	secondUnstable, err := unstable("f.ts", firstUnstable)
	if err != nil {
		t.Fatal(err)
	}
	if firstUnstable == secondUnstable {
		t.Fatalf("the idempotence check cannot detect a non-idempotent transform")
	}
}

// FixFile and FixAndTransformFile with a nil transform must behave identically. FixFile is the
// common path and a nil transform must not become a special case that drifts from it.
func TestFixFileIsFixAndTransformWithNoTransform(t *testing.T) {
	directory := t.TempDir()

	withFixFile := filepath.Join(directory, "a.ts")
	withNilTransform := filepath.Join(directory, "b.ts")
	source := "const a = 'old';\n"

	for _, name := range []string{withFixFile, withNilTransform} {
		if err := os.WriteFile(name, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	firstResult, err := FixFile(withFixFile, proposeWhileContains("'old'", "'new'", "renamer"), DefaultMaxPasses)
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := FixAndTransformFile(withNilTransform, proposeWhileContains("'old'", "'new'", "renamer"), nil, DefaultMaxPasses)
	if err != nil {
		t.Fatal(err)
	}

	if firstResult.Text != secondResult.Text || firstResult.Changed != secondResult.Changed {
		t.Fatalf("the two paths diverged:\n  %+v\n  %+v", firstResult, secondResult)
	}
	if firstResult.Transformed || secondResult.Transformed {
		t.Fatalf("a nil transform reported as having transformed")
	}
}

// The summary counts reformatted files separately from rewritten ones.
func TestSummaryCountsReformattedFiles(t *testing.T) {
	summary := Summarize([]FileResult{
		{FileName: "a.ts", Changed: true, Transformed: true, Passes: 1},
		{FileName: "b.ts", Changed: true, Passes: 1, Applied: []Proposal{{RuleName: "r"}}},
	})

	if summary.FilesTransformed != 1 {
		t.Fatalf("expected 1 reformatted file, got %d", summary.FilesTransformed)
	}
	if !strings.Contains(summary.String(), "1 reformatted") {
		t.Fatalf("the summary hides the reformat count: %s", summary.String())
	}
}

// A transform that declines a file must be recorded as a skip, not as a clean pass.
//
// Asked for by @system_cohere_format: its goja formatter doubles a standalone tilde in markdown,
// turning approximately-25K into strikethrough, so markdown is scoped out until that is fixed. The
// engine does not care which files a transform handles, but the coverage line must not report a
// skipped file the same way it reports a file that was already correctly formatted. Those mean
// opposite things — one says the formatter never looked, the other says it looked and approved.
func TestATransformCanSkipAFileAndTheSkipIsRecorded(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "notes.ts")
	source := "const a = 1;\n"

	if err := os.WriteFile(fileName, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(fileName)
	if err != nil {
		t.Fatal(err)
	}

	skipping := func(_ string, _ string) (string, error) {
		return "", fmt.Errorf("%w: markdown doubles a standalone tilde", ErrSkipped)
	}

	result, err := FixAndTransformFile(fileName, proposeOnce(), skipping, DefaultMaxPasses)
	if err != nil {
		t.Fatalf("a skip must not be an error: %v", err)
	}

	if !result.TransformSkipped {
		t.Fatalf("the skip was not recorded: %+v", result)
	}
	if result.TransformSkipReason != "markdown doubles a standalone tilde" {
		t.Fatalf("the reason was lost or mangled: %q", result.TransformSkipReason)
	}
	if result.Transformed || result.Changed {
		t.Fatalf("a skipped file was reported as changed")
	}

	// A skip is not a refusal. Reporting it as one would send a reader looking for a broken fix.
	for _, rejection := range result.Rejected {
		if strings.HasPrefix(rejection.Reason, ReasonTransformFailed) {
			t.Fatalf("a skip was reported as a transform failure: %+v", rejection)
		}
	}

	after, err := os.Stat(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("a skipped file was rewritten")
	}
}

// A skip must not be confused with a failure. The two send a reader somewhere different: a failure
// says the formatter broke, a skip says it chose not to look.
func TestASkipIsNotAFailure(t *testing.T) {
	skipped := Summarize([]FileResult{
		{FileName: "a.ts", TransformSkipped: true, TransformSkipReason: "markdown"},
	})
	failed := Summarize([]FileResult{
		{FileName: "a.ts", Rejected: []Rejection{{Reason: ReasonTransformFailed + " (boom)"}}},
	})

	if skipped.FilesTransformSkipped != 1 {
		t.Fatalf("the skip was not counted: %+v", skipped)
	}
	if skipped.RefusalsByReason[ReasonTransformFailed] != 0 {
		t.Fatalf("a skip was counted as a transform failure: %+v", skipped.RefusalsByReason)
	}
	if failed.FilesTransformSkipped != 0 {
		t.Fatalf("a failure was counted as a skip: %+v", failed)
	}
	if skipped.String() == failed.String() {
		t.Fatalf("a skipped run and a failed run print the same line: %q", skipped.String())
	}
}

// The summary must distinguish a run that skipped every file from a run where every file was
// already correctly formatted. This is the whole reason the skip channel exists rather than a
// transform returning its input unchanged.
func TestSkippedFilesAreNotReportedAsAlreadyFormatted(t *testing.T) {
	skippedEverything := Summarize([]FileResult{
		{FileName: "a.ts", TransformSkipped: true, TransformSkipReason: "markdown doubles a standalone tilde"},
		{FileName: "b.ts", TransformSkipped: true, TransformSkipReason: "markdown doubles a standalone tilde"},
	})
	alreadyClean := Summarize([]FileResult{
		{FileName: "a.ts"},
		{FileName: "b.ts"},
	})

	if skippedEverything.String() == alreadyClean.String() {
		t.Fatalf("skipping every file reads as every file being clean: %q", alreadyClean.String())
	}

	line := skippedEverything.String()
	if !strings.Contains(line, "2 not formatted") {
		t.Fatalf("the skip count is missing: %s", line)
	}
	// The reason travels with the count, because "2 not formatted" is a number and
	// "2 not formatted (2 markdown doubles a standalone tilde)" is a finding.
	if !strings.Contains(line, "markdown doubles a standalone tilde") {
		t.Fatalf("the skip reason did not reach the summary: %s", line)
	}
}

// A skip with no reason is still recorded. A transform that declines without saying why is worse
// than one that explains itself, but silently dropping the skip would be worse than both.
func TestASkipWithNoReasonIsStillCounted(t *testing.T) {
	directory := t.TempDir()
	fileName := filepath.Join(directory, "bare.ts")

	if err := os.WriteFile(fileName, []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FixAndTransformFile(fileName, proposeOnce(), func(_ string, _ string) (string, error) {
		return "", ErrSkipped
	}, DefaultMaxPasses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.TransformSkipped {
		t.Fatalf("a bare skip was not recorded")
	}
	if result.TransformSkipReason != "" {
		t.Fatalf("a reason was invented: %q", result.TransformSkipReason)
	}
	if !strings.Contains(Summarize([]FileResult{result}).String(), "1 not formatted") {
		t.Fatalf("a reasonless skip vanished from the summary")
	}
}

// The whole summary line is asserted here, not one substring of it.
//
// This fixture exists because a real defect slipped past every other test in this package. Each of
// them checked that its own clause appeared somewhere in the line, and all of them passed while the
// line read:
//
//	... 0 refused, 2 not formatted (2 markdown ...) (1 overlaps another fix) ...
//
// The refusal breakdown had been written to sit directly after the refusal count, and adding the
// reformat and skip clauses pushed it away, so it rendered as a second skip reason. Every number was
// correct and the sentence was wrong, which no substring assertion can see.
//
// So this one compares the rendered line exactly. It is more brittle than the others on purpose:
// the thing being protected is what a person reads, and a change that alters that should have to be
// stated rather than absorbed.
func TestTheWholeSummaryLineReadsCorrectly(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		results []FileResult
		want    string
	}{
		{
			name:    "a run that did nothing states its population",
			results: nil,
			want:    "fix: 0 of 0 files rewritten, 0 fixes applied, 0 refused",
		},
		{
			name: "refusals stay attached to the refusal count",
			results: []FileResult{
				{FileName: "a.ts", Converged: true, Passes: 1, Rejected: []Rejection{{Reason: ReasonOverlap}}},
				{FileName: "b.md", Converged: true, Passes: 1, TransformSkipped: true, TransformSkipReason: "markdown doubles a standalone tilde"},
			},
			want: "fix: 0 of 2 files rewritten, 0 fixes applied, 1 refused (1 overlaps another fix), " +
				"1 not formatted (1 markdown doubles a standalone tilde)",
		},
		{
			name: "a full run reads as one sentence",
			results: []FileResult{
				{FileName: "a.ts", Converged: true, Changed: true, Passes: 2, Applied: []Proposal{{RuleName: "r"}, {RuleName: "r"}}, Transformed: true},
				{FileName: "b.ts", Converged: true, Changed: true, Passes: 1, Applied: []Proposal{{RuleName: "r"}}},
				{FileName: "c.md", Converged: true, Passes: 1, TransformSkipped: true, TransformSkipReason: "markdown doubles a standalone tilde"},
				{FileName: "d.ts", Failed: true},
			},
			want: "fix: 2 of 4 files rewritten, 3 fixes applied, 0 refused, 1 reformatted, " +
				"1 not formatted (1 markdown doubles a standalone tilde), up to 2 passes, " +
				"1 files could not be processed",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := Summarize(testCase.results).String()
			if got != testCase.want {
				t.Fatalf("the summary line reads wrong:\n  want %q\n  got  %q", testCase.want, got)
			}
		})
	}
}

// CheckFile is FixAndTransformFile without the write: the same result, the file untouched. It is what
// `--no-fix` reports, so a result that differed from the writing run's would report a tree as clean
// that `--fix` then rewrites.
func TestCheckFileComputesTheWriteAndLeavesTheFile(t *testing.T) {
	directory := t.TempDir()
	original := "const a = 'old';\n"
	checked := filepath.Join(directory, "checked.ts")
	written := filepath.Join(directory, "written.ts")
	for _, fileName := range []string{checked, written} {
		if err := os.WriteFile(fileName, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	transform := func(_ string, text string) (string, error) {
		return strings.ReplaceAll(text, "const", "export const"), nil
	}

	checkResult, err := CheckFile(checked, proposeWhileContains("'old'", "'new'", "renamer"), transform, DefaultMaxPasses)
	if err != nil {
		t.Fatal(err)
	}
	writeResult, err := FixAndTransformFile(written, proposeWhileContains("'old'", "'new'", "renamer"), transform, DefaultMaxPasses)
	if err != nil {
		t.Fatal(err)
	}

	if contents, _ := os.ReadFile(checked); string(contents) != original {
		t.Fatalf("CheckFile wrote to disk:\n%s", contents)
	}
	if contents, _ := os.ReadFile(written); checkResult.Text != string(contents) || !checkResult.Changed || !checkResult.Transformed {
		t.Fatalf("the check and the write disagree:\n  checked %q (changed %v, transformed %v)\n  written %q",
			checkResult.Text, checkResult.Changed, checkResult.Transformed, contents)
	}

	// The summary names the file and what changed it, the rule before the formatter, and a checked
	// run says so in the conditional rather than claiming a rewrite.
	summary := Summarize([]FileResult{checkResult})
	summary.Checked = true
	if len(summary.ChangedFiles) != 1 || summary.ChangedFiles[0].FileName != checked ||
		strings.Join(summary.ChangedFiles[0].Changers, ",") != "renamer,format" {
		t.Fatalf("the changed file was not named with what changed it: %+v", summary.ChangedFiles)
	}
	if line := summary.String(); !strings.Contains(line, "1 of 1 files would be rewritten, 1 fixes would apply") ||
		!strings.Contains(line, "1 would be reformatted") {
		t.Fatalf("a checked summary claims a rewrite: %s", line)
	}
	if line := Summarize([]FileResult{writeResult}).String(); !strings.Contains(line, "1 of 1 files rewritten, 1 fixes applied") {
		t.Fatalf("the writing run's line moved: %s", line)
	}
}
