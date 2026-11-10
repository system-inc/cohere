package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// measuredText is what a walk read: `let alpha` at the start, which a prefer-const fix rewrites at bytes 0 to 3.
const measuredText = "let alpha = 1;\n"

// changedText is the file after another agent added a line above it. Bytes 0 to 3 are now `var`, so the walk's fix
// applied here still parses, and writes `const other`, which nobody asked for.
const changedText = "var other = 2;\nlet alpha = 1;\n"

// proposeFromAStaleWalk answers its first ask with the walk's proposal, measured against measuredText, and every
// later ask as a proposer must, against the text it is handed.
func proposeFromAStaleWalk() Propose {
	asked := false
	return func(_ string, text string) ([]Proposal, error) {
		if !asked {
			asked = true
			return []Proposal{{
				RuleName:   "prefer-const",
				Fix:        rule.Fix{Text: "const", Range: core.NewTextRange(0, 3)},
				MeasuredOn: DigestOf(measuredText),
			}}, nil
		}
		position := strings.Index(text, "let alpha")
		if position < 0 {
			return nil, nil
		}
		return []Proposal{{
			RuleName: "prefer-const",
			Fix:      rule.Fix{Text: "const", Range: core.NewTextRange(position, position+3)},
		}}, nil
	}
}

// A fix measured against text the file no longer holds is refused, never applied, and the file's fixes are asked
// for again against the text in hand (#bxmcf34). Before the guard the walk's offsets landed on `var` and wrote
// `const other = 2;`, which parses, so nothing refused it. Fails without the check in fixText.
func TestAProposalMeasuredAgainstOtherTextIsRefusedAndMeasuredAgain(test *testing.T) {
	test.Parallel()
	fileName := filepath.Join(test.TempDir(), "racing.ts")
	if writeError := os.WriteFile(fileName, []byte(changedText), 0o644); writeError != nil {
		test.Fatal(writeError)
	}

	result, fixError := FixFile(rootOf(test, fileName), fileName, proposeFromAStaleWalk(), DefaultMaxPasses)
	if fixError != nil {
		test.Fatal(fixError)
	}
	written, readError := os.ReadFile(fileName)
	if readError != nil {
		test.Fatal(readError)
	}
	if want := "var other = 2;\nconst alpha = 1;\n"; string(written) != want {
		test.Fatalf("the file holds:\n%s\nwant:\n%s", written, want)
	}
	if !result.ChangedDuringRun {
		test.Fatal("the file changed under the run and the result does not say so")
	}
	refused := false
	for _, rejection := range result.Rejected {
		refused = refused || rejection.Reason == ReasonChangedSinceMeasured
	}
	if !refused {
		test.Fatalf("the stale proposal was not refused as stale: %+v", result.Rejected)
	}
	if summary := Summarize([]FileResult{result}); len(summary.ChangedDuringRun) != 1 ||
		!strings.Contains(summary.String(), "1 changed during the run (fixes measured again)") {
		test.Fatalf("the summary does not name the file: %v, %q", summary.ChangedDuringRun, summary.String())
	}
}

// A proposal measured against exactly the text in hand lands as any other, and says nothing changed: the guard
// refuses by what a proposal was measured against, never because it carries a digest at all.
func TestAProposalMeasuredAgainstTheTextInHandLands(test *testing.T) {
	test.Parallel()
	fileName := filepath.Join(test.TempDir(), "steady.ts")
	if writeError := os.WriteFile(fileName, []byte(measuredText), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	asked := false
	propose := func(_ string, _ string) ([]Proposal, error) {
		if asked {
			return nil, nil
		}
		asked = true
		return []Proposal{{
			RuleName:   "prefer-const",
			Fix:        rule.Fix{Text: "const", Range: core.NewTextRange(0, 3)},
			MeasuredOn: DigestOf(measuredText),
		}}, nil
	}

	result, fixError := FixFile(rootOf(test, fileName), fileName, propose, DefaultMaxPasses)
	if fixError != nil {
		test.Fatal(fixError)
	}
	if !result.Changed || result.ChangedDuringRun || result.Text != "const alpha = 1;\n" {
		test.Fatalf("want the fix landed and nothing said to change, got %+v", result)
	}
}
