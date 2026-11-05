package shouting

import (
	"strings"
	"testing"
)

// MaskCodeAndCommands keeps the input's byte length and its newlines where they were, whatever the masked code
// holds, because the Go engine masks a whole comment group and slices the result by each comment's byte offsets. A
// mask that blanked a multi-byte rune to one space shrank the text, and the engine read past its end. Each case puts
// multi-byte runes inside one mask's shape.
func TestMaskKeepsByteLengthAndNewlines(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name string
		text string
	}{
		{"inline backticks", "supports `www.點看.com`, and NEVER"},
		{"backticks that wrap", "the `點\n看` branch is NEVER"},
		{"fenced block", "```\n點看 é\n```\nthen NEVER"},
		{"double quotes", `the "點看 é" label is NEVER`},
		{"double quote that wraps", "the \"點\n看\" is NEVER"},
		{"indented code block", "Run it:\n\n    點看 é\n\nthen NEVER"},
		{"example block", " * @example\n * 點看 é\n * @returns NEVER"},
		{"command line", "git commit -m 點看\nthen NEVER"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			masked := MaskCodeAndCommands(testCase.text)
			if len(masked) != len(testCase.text) {
				subtest.Fatalf("masking %q changed its length from %d to %d bytes: %q",
					testCase.text, len(testCase.text), len(masked), masked)
			}
			for index := range len(testCase.text) {
				if (testCase.text[index] == '\n') != (masked[index] == '\n') {
					subtest.Fatalf("masking %q moved a newline at byte %d: %q", testCase.text, index, masked)
				}
			}
			if !strings.HasSuffix(masked, "NEVER") {
				subtest.Fatalf("masking %q blanked the prose after the code: %q", testCase.text, masked)
			}
		})
	}
}
