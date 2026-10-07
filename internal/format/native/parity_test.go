package native

import (
	"testing"

	"github.com/system-inc/cohere/internal/format/formatoptions"
	"github.com/system-inc/cohere/internal/format/oracletest"
)

// TestEmptyTextAndNumericSeparatorsMatchTheFork holds the nine inputs @system_adamic's JSON audit found
// cohere and Prettier 3.9.6 disagree on (#w6vtsrn), and their neighbours, to the fork's recorded answers:
// the same output, or a refusal on both sides.
//
// Empty or whitespace-only text is core.js's to answer, before any parser, so it is asked of every
// language. A numeric separator is Babel's to judge, so the JSON cases run under both of JSON's parsers,
// json (probe.json) and json-stringify (package.json), well-formed separators beside malformed ones.
func TestEmptyTextAndNumericSeparatorsMatchTheFork(t *testing.T) {
	t.Parallel()
	type input struct{ fileName, text string }
	var inputs []input
	for _, fileName := range []string{"probe.json", "package.json", "probe.ts", "probe.tsx", "probe.js", "probe.md",
		"probe.css", "probe.graphql", "probe.yaml"} {
		for _, text := range []string{"", " ", "\n", "\n\n\t \n", "\u00a0\u2028\ufeff", "\ufeff", "\ufeff\n"} {
			inputs = append(inputs, input{fileName, text})
		}
	}
	for _, fileName := range []string{"probe.json", "package.json"} {
		for _, text := range []string{
			// The audit's three, refused by Prettier.
			"[1__0]", "[1_]", "[0x_1]",
			// Malformed elsewhere in a literal.
			"[1_.5]", "[1._5]", "[1_e5]", "[1e_5]", "[1e5_]", "[.5_]", "[0x1_]", "[0x1__f]", "[0b_1]", "[0o_7]",
			"[0_1]", "[1_0n]",
			// Well-formed, which Prettier accepts or refuses on its own terms; the recorded answer says which.
			"[1_0]", "[1_000_000]", "[1_0.5_5]", "[1e1_0]", "[1e+1_0]", "[1e+_10]", "[0x1_f]", "[0xa_b]",
			"[0b1_0]", "[0o7_1]", "[.5_5]", "{\"a\": 1_0}", "{1_0: 1}",
		} {
			inputs = append(inputs, input{fileName, text + "\n"})
		}
	}

	options := formatoptions.Default()
	oracle := oracletest.Open(t, t.Name()).Engine(options)
	formatter := Formatter{Options: options}
	compared, refusedByBoth := 0, 0
	for _, each := range inputs {
		expected, oracleErr := oracle.Format(each.fileName, each.text)
		actual, nativeErr := formatter.Format(each.fileName, each.text)
		switch {
		case oracleErr != nil && nativeErr != nil:
			refusedByBoth++
		case oracleErr != nil:
			t.Errorf("%s %q: the fork refuses it (%v) and Formatter printed %q", each.fileName, each.text, oracleErr, actual)
		case nativeErr != nil:
			t.Errorf("%s %q: the fork prints %q and Formatter refused: %v", each.fileName, each.text, expected, nativeErr)
		default:
			compared++
			if actual != expected {
				t.Errorf("%s %q: the fork prints %q and Formatter %q", each.fileName, each.text, expected, actual)
			}
		}
	}
	// Both answers must occur, so a side that refuses everything, or nothing, can't read as agreement.
	if compared == 0 || refusedByBoth == 0 {
		t.Errorf("%d compared and %d refused by both: one kind of answer is missing", compared, refusedByBoth)
	}
	t.Logf("%d inputs: %d printed alike, %d refused by both", len(inputs), compared, refusedByBoth)
}
