package fix

import (
	"errors"
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
