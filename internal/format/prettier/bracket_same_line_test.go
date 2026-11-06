package prettier

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/format_options"
)

// TestBracketSameLineReachesTheEngine proves the option is passed, not merely carried.
//
// A field resolved and never handed to Prettier would pass every resolution test in format_options.
// This formats the same JSX both ways and requires different bytes, the specific difference being
// where `>` goes.
func TestBracketSameLineReachesTheEngine(test *testing.T) {
	test.Parallel()

	source := "const element = <Component firstAttribute=\"a long value here\" secondAttribute=\"another long value\" third=\"x\">child</Component>;\n"

	format := func(sameLine bool) string {
		options := format_options.Default()
		options.PrintWidth = 60
		options.BracketSameLine = sameLine
		engine, newPrettierError := New(options)
		if newPrettierError != nil {
			test.Fatal(newPrettierError)
		}
		formatted, formatError := engine.Format("probe.tsx", source)
		if formatError != nil {
			test.Fatal(formatError)
		}
		return formatted
	}

	apart, together := format(false), format(true)
	if apart == together {
		test.Fatalf("bracketSameLine changed nothing:\n%s", apart)
	}
	if !strings.Contains(together, "\"x\">") || strings.Contains(apart, "\"x\">") {
		test.Fatalf(
			"expected `>` on the attribute line only with bracketSameLine:\nfalse:\n%s\ntrue:\n%s",
			apart,
			together,
		)
	}
}
