package prettier

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/formatoptions"
)

// TestBracketSameLineReachesTheEngine proves the option is passed, not merely carried.
//
// A field resolved and never handed to Prettier would pass every resolution test in formatoptions.
// This formats the same JSX both ways and requires different bytes, the specific difference being
// where `>` goes.
func TestBracketSameLineReachesTheEngine(test *testing.T) {
	test.Parallel()

	source := "const element = <Component firstAttribute=\"a long value here\" secondAttribute=\"another long value\" third=\"x\">child</Component>;\n"

	format := func(sameLine bool) string {
		options := formatoptions.Default()
		options.PrintWidth = 60
		options.BracketSameLine = sameLine
		engine, err := New(options)
		if err != nil {
			test.Fatal(err)
		}
		formatted, err := engine.Format("probe.tsx", source)
		if err != nil {
			test.Fatal(err)
		}
		return formatted
	}

	apart, together := format(false), format(true)
	if apart == together {
		test.Fatalf("bracketSameLine changed nothing:\n%s", apart)
	}
	if !strings.Contains(together, "\"x\">") || strings.Contains(apart, "\"x\">") {
		test.Fatalf("expected `>` on the attribute line only with bracketSameLine:\nfalse:\n%s\ntrue:\n%s", apart, together)
	}
}
