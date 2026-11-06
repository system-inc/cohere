package main

import (
	"strings"
	"testing"
)

/*
 * A finding prints as one line, whatever its message does internally.
 *
 * Two rules write a second paragraph into their description with an embedded newline:
 * `no-misused-spread` and `no-unsafe-assignment`. The printer emitted that newline verbatim, so the
 * rule tag landed on a continuation line carrying no position.
 *
 * The cost is silent and it is biased. Any reader keyed on `position ... [rule]` drops those
 * findings, and it drops the longest messages, which are the ones carrying advice. Four of 5,312
 * findings on the ahra tree wrapped that way. An extractor of mine missed them and I spent four
 * hours instrumenting a rule that was working correctly, chasing a gap that existed only in the
 * tool reading the output.
 *
 * This test asserts the property rather than the two rules, because the next rule to want a second
 * paragraph should not have to know about it. The message is the rule's to write; the line
 * discipline is the printer's to keep.
 */
func TestDiagnosticDescriptionIsASingleLine(test *testing.T) {
	test.Parallel()

	for name, testCase := range map[string]struct {
		description string
		want        string
	}{
		"plain": {
			description: "Unsafe member access .value on an `any` value.",
			want:        "Unsafe member access .value on an `any` value.",
		},
		"twoParagraphs": {
			description: "Using the spread operator on a string can mishandle characters.\n" +
				"Consider `Intl.Segmenter` instead.",
			want: "Using the spread operator on a string can mishandle characters. " +
				"Consider `Intl.Segmenter` instead.",
		},
		"severalNewlines": {
			description: "first\nsecond\nthird",
			want:        "first second third",
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			got := singleLineDescription(testCase.description)
			if got != testCase.want {
				test.Errorf("description = %q, want %q", got, testCase.want)
			}
			if strings.Contains(got, "\n") {
				test.Errorf("description still contains a newline: %q", got)
			}
		})
	}
}
