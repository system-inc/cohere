package go_engine

import (
	"bytes"
	"fmt"
	goformat "go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_file"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_useless_concatenation"
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

// The wrap never takes a file past the long-file limit: a file the rule does not report before the wrap and would
// after it keeps gofmt's output, declined with why, while the same file with room for the wrap is wrapped. A file
// already past the limit, and one whose generated name the rule exempts, may still be wrapped, since the wrap creates
// no finding there.
func TestTheWrapNeverTakesAFilePastTheLongFileLimit(test *testing.T) {
	test.Parallel()

	formatted := []byte("package fixture\n\n" + longCallBody)
	wrapping := wrapLongLines(formatted)
	if wrapping.Output == nil {
		test.Fatalf("the fixture's call was not wrapped: %q", wrapping.Declined)
	}
	before := consistency_no_long_file.LineCount(formatted)
	after := consistency_no_long_file.LineCount(wrapping.Output)
	for _, testCase := range []struct {
		name     string
		file     string
		limit    int
		declined bool
	}{
		{name: "one line short of room", file: "report.go", limit: after - 1, declined: true},
		{name: "room for the wrap", file: "report.go", limit: after},
		{name: "already past the limit", file: "report.go", limit: before - 1},
		{name: "a generated name", file: "report.generated.go", limit: after - 1},
	} {
		held := heldToLongFileLimit(testCase.file, formatted, wrapping, testCase.limit)
		if testCase.declined {
			if held.Output != nil || !strings.Contains(held.Declined, "past the long-file limit") {
				test.Errorf("%s: want the wrap declined past the limit, got %d lines, %q", testCase.name, held.Lines,
					held.Declined)
			}
			continue
		}
		if held.Declined != "" || !bytes.Equal(held.Output, wrapping.Output) || held.Lines != wrapping.Lines {
			test.Errorf("%s: want the wrap kept, got %d lines, %q", testCase.name, held.Lines, held.Declined)
		}
	}
}

// The wrap hides no other rule's finding: over the long-lines fixture, whose Greeting joins two string literals with a
// + on a long line, useless-concatenation reports as many joins after a writing run as before it, though the run
// wrapped the line. That rule leaves alone a join across lines, so a break between the two literals would drop its
// finding while fixing nothing it names.
func TestTheWrapHidesNoUselessConcatenation(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "long-lines"))
	file := filepath.Join(root, "long_lines.go")
	joins := func(extra ...string) int {
		test.Helper()
		var standardOutput bytes.Buffer
		var standardError bytes.Buffer
		arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-cache"},
			extra...)
		if exitCode := run(arguments, &standardOutput, &standardError, rules.All()); exitCode != exitFindings {
			test.Fatalf("exit %d, want %d; stderr:\n%s", exitCode, exitFindings, standardError.String())
		}
		return strings.Count(standardOutput.String(), `"rule":"`+consistency_no_useless_concatenation.Name+`"`)
	}
	original, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatalf("reading %s: %v", file, readError)
	}
	before := joins("--no-fix")
	joins()
	after := joins("--no-fix")
	wrapped, readError := os.ReadFile(file)
	if readError != nil || bytes.Equal(wrapped, original) {
		test.Fatalf("the writing run left %s as it was (%v)", file, readError)
	}
	if before < 1 || after != before {
		test.Fatalf("useless-concatenation reported %d joins before the wrap and %d after, want the same, at least "+
			"one:\n%s", before, after, wrapped)
	}
	if !strings.Contains(string(wrapped), `", and welcome to the depot's " + "own courier service in " +`) {
		test.Errorf("want the two joined literals kept on one line, got:\n%s", wrapped)
	}
}
