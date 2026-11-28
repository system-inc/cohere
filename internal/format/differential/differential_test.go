package differential

import (
	"strings"
	"testing"
)

// TestFirstDifferenceQuotesWhitespace pins why both lines are quoted: a trailing space must be visible.
func TestFirstDifferenceQuotesWhitespace(test *testing.T) {
	test.Parallel()
	got := FirstDifference("a\nb\n", "a\nb \n")
	if !strings.Contains(got, `"b"`) || !strings.Contains(got, `"b "`) || !strings.Contains(got, "line 2") {
		test.Fatalf("FirstDifference = %q", got)
	}
}
