package rule

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/core"
)

// The fix helpers are pure range arithmetic, and getting one off by a byte silently corrupts source
// rather than failing loudly. These pin the arithmetic so a refactor cannot quietly move it.
func TestFixHelperRanges(test *testing.T) {
	test.Parallel()
	// A node spanning bytes [10, 20).
	span := core.NewTextRange(10, 20)

	test.Run("ReplaceRange keeps the span and carries the text", func(test *testing.T) {
		test.Parallel()
		fix := ReplaceRange(span, "replacement")
		if fix.Range.Pos() != 10 || fix.Range.End() != 20 {
			test.Fatalf("expected [10,20), got [%d,%d)", fix.Range.Pos(), fix.Range.End())
		}
		if fix.Text != "replacement" {
			test.Fatalf("expected the replacement text, got %q", fix.Text)
		}
	})

	test.Run("a deletion is an empty replacement", func(test *testing.T) {
		test.Parallel()
		fix := ReplaceRange(span, "")
		if fix.Text != "" {
			test.Fatalf("expected empty text for a deletion, got %q", fix.Text)
		}
		if fix.Range.End()-fix.Range.Pos() != 10 {
			test.Fatalf("a deletion must still cover the whole span, got %d bytes", fix.Range.End()-fix.Range.Pos())
		}
	})

	test.Run("an insertion point is empty, so it overwrites nothing", func(test *testing.T) {
		test.Parallel()
		// This is the property that matters: an insertion must be a zero-width range. A range of
		// any width would replace the bytes it covers, which is how an "insert" silently becomes a
		// deletion of whatever it was inserted next to.
		before := Fix{Range: span.WithEnd(span.Pos()), Text: "prefix"}
		if before.Range.Pos() != before.Range.End() {
			test.Fatalf("insert-before must be zero-width, got [%d,%d)", before.Range.Pos(), before.Range.End())
		}
		if before.Range.Pos() != 10 {
			test.Fatalf("insert-before must sit at the node start, got %d", before.Range.Pos())
		}

		after := Fix{Range: span.WithPos(span.End()), Text: "suffix"}
		if after.Range.Pos() != after.Range.End() {
			test.Fatalf("insert-after must be zero-width, got [%d,%d)", after.Range.Pos(), after.Range.End())
		}
		if after.Range.Pos() != 20 {
			test.Fatalf("insert-after must sit at the node end, got %d", after.Range.Pos())
		}
	})
}

// A rule that returns no listeners has declined the file, and that must be distinguishable from a
// rule that listened and found nothing. The first costs nothing; the second walked the tree.
func TestRuleMayDeclineAFile(test *testing.T) {
	test.Parallel()
	declining := Rule{
		Name: "declines-everything",
		Run: func(ctx Context, options any) Listeners {
			return nil
		},
	}

	if got := declining.Run(Context{}, nil); got != nil {
		test.Fatalf("expected nil listeners from a declining rule, got %d", len(got))
	}
}

// A range report carries its suggestions, and the range it was given.
//
// The node helpers came in three shapes and the range helpers in one, so a rule reporting a sub-range
// of a string literal with suggestions had to hand-build a Diagnostic. That works, and it is the
// wrong thing to make a rule author do: every hand-rolled Diagnostic is a place the Range can be
// built from `Loc` instead of `TokenRange` without anything downstream noticing.
func TestReportRangeWithSuggestionsCarriesBoth(test *testing.T) {
	test.Parallel()
	reported := []Diagnostic{}
	context := Context{Report: func(diagnostic Diagnostic) {
		reported = append(reported, diagnostic)
	}}

	wanted := core.NewTextRange(12, 16)
	context.ReportRangeWithSuggestions(wanted, Message{Id: "probe"},
		Suggestion{Message: Message{Id: "repair"}})

	if len(reported) != 1 {
		test.Fatalf("want one diagnostic, got %d", len(reported))
	}
	if reported[0].Range != wanted {
		test.Errorf("the range was not carried:\n  got  %v\n  want %v", reported[0].Range, wanted)
	}
	if len(reported[0].Suggestions) != 1 {
		test.Fatalf("want one suggestion, got %d", len(reported[0].Suggestions))
	}
	if reported[0].Suggestions[0].Message.Id != "repair" {
		test.Errorf("the suggestion was not carried: %q", reported[0].Suggestions[0].Message.Id)
	}
}

// The other direction: it offers no fixes.
//
// A suggestion needs a human to choose it and a fix does not, and the fix engine applies only the
// second. A helper that quietly populated Fixes would make every suggestion an automatic rewrite,
// which is the one distinction this whole API exists to hold.
func TestReportRangeWithSuggestionsProposesNoFixes(test *testing.T) {
	test.Parallel()
	reported := []Diagnostic{}
	context := Context{Report: func(diagnostic Diagnostic) {
		reported = append(reported, diagnostic)
	}}

	context.ReportRangeWithSuggestions(core.NewTextRange(0, 1), Message{Id: "probe"},
		Suggestion{Message: Message{Id: "repair"}})

	if len(reported) != 1 {
		test.Fatalf("want one diagnostic, got %d", len(reported))
	}
	if len(reported[0].Fixes) != 0 {
		test.Errorf("a suggestion was carried as an applicable fix: %d fixes", len(reported[0].Fixes))
	}
}
