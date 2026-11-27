package markdown

import (
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
)

// delimiterSweep is the slice of TestExhaustiveDelimitersMatchOracle's sweep that is recorded: every string of up
// to three of `a`, `*`, `_`, space and `.`, bare, inside emphasis and strong, and between two words, 775 inputs.
// The escapes word.js adds to delimiter runs depend on which side of a run is flanking, and three characters
// already put a run at the start, the end and the middle of a word. The whole sweep went to five characters,
// some 19,500 inputs against the live oracle, an opt-in run; its answers would have been many times the size a
// committed file may be.
func delimiterSweep() []string {
	alphabet := []string{"a", "*", "_", " ", "."}
	var inputs []string
	var extend func(prefix string, length int)
	extend = func(prefix string, length int) {
		if prefix != "" {
			inputs = append(inputs, prefix, "*"+prefix+"*", "**"+prefix+"**", "_"+prefix+"_", "x "+prefix+" y")
		}
		if length == 3 {
			return
		}
		for _, letter := range alphabet {
			extend(prefix+letter, length+1)
		}
	}
	extend("", 0)
	return inputs
}

// TestDelimiterSweepMatchesOracle formats every input of the sweep's slice and compares it with the embedded
// Prettier's recorded answer, under the default options as the sweep ran. An input the oracle refuses was
// skipped by the sweep, and is here.
func TestDelimiterSweepMatchesOracle(test *testing.T) {
	test.Parallel()
	inputs := delimiterSweep()
	if len(inputs) != 775 {
		test.Fatalf("the sweep's slice holds %d inputs, not the 775 that were recorded", len(inputs))
	}
	// The oracle's answers, recorded (see oracle_testing), so no JavaScript runs here.
	formatter := oracle_testing.Open(test, test.Name()).Formatter(format_options.Default())
	failures := 0
	for _, input := range inputs {
		expected, oracleError := formatter.Format("fixture.md", input)
		if oracleError != nil {
			continue
		}
		actual, formatError := Format(input, format_options.Default(), nil)
		if formatError != nil || actual != expected {
			failures++
			if failures <= 20 {
				test.Errorf("%q: %v %s", input, formatError, differential.FirstDifference(expected, actual))
			}
		}
	}
	if failures > 0 {
		test.Errorf("%d of %d inputs differ", failures, len(inputs))
	}
}
