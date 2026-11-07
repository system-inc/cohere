package go_engine

import (
	goformat "go/format"
	"strings"
	"testing"
)

// longCallBody is a function whose one statement is a call past the limit that a wrap breaks one argument per line.
const longCallBody = "func Report() {\n\treport(context, \"the description of what went wrong here, in a sentence\", " +
	"firstArgumentName, secondArgumentName, third)\n}\n"

// The wrap acts on a file the rule's findings reach, and leaves alone a copy of upstream source, whose findings the
// driver withholds, and a file whose directive withholds the rule, since the wrap would move the lines it covers. A
// directive for another rule changes nothing. Each source is gofmt's, as the fix phase hands it over.
func TestWrapLongLinesLeavesWhatTheRuleDoesNotReport(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name   string
		source string
		lines  int
	}{
		{name: "a file the rule reports", source: "package fixture\n\n" + longCallBody, lines: 1},
		{
			name: "a copy of upstream source",
			source: "// cohere:copied golang.org/x/tools@v0.50.0 go/analysis/checker/checker.go\n" +
				"// cohere:until upstream exports what we need\n\npackage fixture\n\n" + longCallBody,
		},
		{
			name: "a file-ignore of the rule",
			source: "//cohere:file-ignore consistency-no-long-line every case on one line, by design\n\n" +
				"package fixture\n\n" + longCallBody,
		},
		{
			name: "a line ignore of the rule, in full",
			source: "package fixture\n\nfunc Report() {\n\t//cohere:ignore cohere-go/consistency-no-long-line one " +
				"line, by design\n" + strings.TrimPrefix(longCallBody, "func Report() {\n"),
		},
		{
			name: "a file-ignore of another rule",
			source: "//cohere:file-ignore printf the format is built at run time\n\npackage fixture\n\n" +
				longCallBody,
			lines: 1,
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			formatted, formatError := goformat.Source([]byte(testCase.source))
			if formatError != nil || string(formatted) != testCase.source {
				test.Fatalf("the source is not gofmt's (%v):\n%s", formatError, formatted)
			}
			wrapping := wrapLongLines(formatted)
			if wrapping.Declined != "" || wrapping.Lines != testCase.lines ||
				(wrapping.Output != nil) != (testCase.lines > 0) {
				test.Fatalf("want %d lines wrapped, got %d, declined %q, output:\n%s", testCase.lines,
					wrapping.Lines, wrapping.Declined, wrapping.Output)
			}
		})
	}
}
