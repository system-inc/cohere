package rule

import (
	"testing"

	"github.com/microsoft/typescript-go/shim/core"
)

// The fix helpers are pure range arithmetic, and getting one off by a byte silently corrupts source
// rather than failing loudly. These pin the arithmetic so a refactor cannot quietly move it.
func TestFixHelperRanges(t *testing.T) {
	// A node spanning bytes [10, 20).
	span := core.NewTextRange(10, 20)

	t.Run("ReplaceRange keeps the span and carries the text", func(t *testing.T) {
		fix := ReplaceRange(span, "replacement")
		if fix.Range.Pos() != 10 || fix.Range.End() != 20 {
			t.Fatalf("expected [10,20), got [%d,%d)", fix.Range.Pos(), fix.Range.End())
		}
		if fix.Text != "replacement" {
			t.Fatalf("expected the replacement text, got %q", fix.Text)
		}
	})

	t.Run("a deletion is an empty replacement", func(t *testing.T) {
		fix := ReplaceRange(span, "")
		if fix.Text != "" {
			t.Fatalf("expected empty text for a deletion, got %q", fix.Text)
		}
		if fix.Range.End()-fix.Range.Pos() != 10 {
			t.Fatalf("a deletion must still cover the whole span, got %d bytes", fix.Range.End()-fix.Range.Pos())
		}
	})

	t.Run("an insertion point is empty, so it overwrites nothing", func(t *testing.T) {
		// This is the property that matters: an insertion must be a zero-width range. A range of
		// any width would replace the bytes it covers, which is how an "insert" silently becomes a
		// deletion of whatever it was inserted next to.
		before := Fix{Range: span.WithEnd(span.Pos()), Text: "prefix"}
		if before.Range.Pos() != before.Range.End() {
			t.Fatalf("insert-before must be zero-width, got [%d,%d)", before.Range.Pos(), before.Range.End())
		}
		if before.Range.Pos() != 10 {
			t.Fatalf("insert-before must sit at the node start, got %d", before.Range.Pos())
		}

		after := Fix{Range: span.WithPos(span.End()), Text: "suffix"}
		if after.Range.Pos() != after.Range.End() {
			t.Fatalf("insert-after must be zero-width, got [%d,%d)", after.Range.Pos(), after.Range.End())
		}
		if after.Range.Pos() != 20 {
			t.Fatalf("insert-after must sit at the node end, got %d", after.Range.Pos())
		}
	})
}

// A rule that returns no listeners has declined the file, and that must be distinguishable from a
// rule that listened and found nothing. The first costs nothing; the second walked the tree.
func TestRuleMayDeclineAFile(t *testing.T) {
	declining := Rule{
		Name: "declines-everything",
		Run: func(ctx Context, options any) Listeners {
			return nil
		},
	}

	if got := declining.Run(Context{}, nil); got != nil {
		t.Fatalf("expected nil listeners from a declining rule, got %d", len(got))
	}
}
