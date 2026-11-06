package consistency_no_long_file_test

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_long_file"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_long_file.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-long-file"); parseError != nil {
		test.Fatal(parseError)
	}
}

// At the house limit, the flagged fixture of 2,001 lines carries a want at the start of its last line, and
// analysistest fails on a finding no want expects as much as on a want nothing found, so the allowed and lookalike
// fixtures prove the rule stays quiet on a file of exactly 2,000 lines ending in a newline, one of 2,000 with no
// final newline, and a short file a //line directive numbers past the limit.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_long_file.NewAnalyzer(),
		"flagged",
		"allowed",
		"lookalike",
	)
}

// With max set to 50, a file of 51 lines is flagged at its 51st and its neighbour of exactly 50 is not, so the
// option is read from the analyzer running the pass and a stricter limit moves the finding with it.
func TestAnalyzerWithAStricterLimit(test *testing.T) {
	test.Parallel()
	analyzer := consistency_no_long_file.NewAnalyzer()
	if setError := analyzer.Flags.Set("max", "50"); setError != nil {
		test.Fatal(setError)
	}
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), analyzer, "stricter")
}

// A limit that is not a whole number of lines, one or more, is refused when it is set, so a bad setting fails the
// engine's configuration instead of flagging every file or naming a line no file has.
func TestLimitRefusesWhatIsNotALineCount(test *testing.T) {
	test.Parallel()
	for _, text := range []string{"0", "-1", "", "many", "2000.5"} {
		analyzer := consistency_no_long_file.NewAnalyzer()
		if setError := analyzer.Flags.Set("max", text); setError == nil {
			test.Errorf("setting the limit to %q was accepted", text)
		}
	}
}

// The max option may only make the rule stricter, so 2,001 is refused with an error naming the option and the value,
// which the engine prints as the run's refusal, while 1,500 and the house limit itself are taken and read back.
func TestLimitRefusesALooserLimit(test *testing.T) {
	test.Parallel()
	looser := consistency_no_long_file.NewAnalyzer()
	setError := looser.Flags.Set("max", "2001")
	if setError == nil {
		test.Fatal("setting the limit to 2001 was accepted")
	}
	if !strings.Contains(setError.Error(), "max option of 2001 lines") {
		test.Errorf("the refusal does not name the option and the value: %v", setError)
	}
	for _, text := range []string{"1500", "2000"} {
		stricter := consistency_no_long_file.NewAnalyzer()
		if setError := stricter.Flags.Set("max", text); setError != nil {
			test.Fatalf("setting the limit to %s was refused: %v", text, setError)
		}
		if read := stricter.Flags.Lookup("max").Value.String(); read != text {
			test.Errorf("the limit set to %s reads back as %s", text, read)
		}
	}
}

// A file over the limit whose base name has the `*.generated.*` shape, long.generated.go with no generated header,
// gets no finding, while its neighbour generated_long.go, the word without the shape, carries a want, so the
// exemption is by the shape of the name and not by the word in it.
func TestAnalyzerExemptsAGeneratedName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_long_file.NewAnalyzer(), "named")
}

// The finding runs from the start of the first line over the limit to the end of the file, the offset of its last
// byte as written, which the wants above can't see, since analysistest matches a want by its line alone. Both a file
// at the house limit and one at a stricter limit end there.
func TestFindingRunsToTheEndOfTheFile(test *testing.T) {
	test.Parallel()
	stricter := consistency_no_long_file.NewAnalyzer()
	if setError := stricter.Flags.Set("max", "50"); setError != nil {
		test.Fatal(setError)
	}
	for _, run := range []struct {
		name      string
		analyzer  *analysis.Analyzer
		firstLine int
	}{
		{"flagged", consistency_no_long_file.NewAnalyzer(), 2001},
		{"stricter", stricter, 51},
	} {
		results := analysistest.Run(errorsOnly{test}, analysistest.TestData(), run.analyzer, run.name)
		reported := 0
		for _, result := range results {
			for _, diagnostic := range result.Diagnostics {
				reported++
				start := result.Pass.Fset.PositionFor(diagnostic.Pos, false)
				if start.Line != run.firstLine || start.Column != 1 {
					test.Errorf("%s's finding starts at %d:%d, not at the start of line %d", run.name, start.Line,
						start.Column, run.firstLine)
				}
				if !diagnostic.End.IsValid() {
					test.Errorf("%s's finding at %s has no end", run.name, start)
					continue
				}
				end := result.Pass.Fset.PositionFor(diagnostic.End, false)
				contents, readError := os.ReadFile(end.Filename)
				if readError != nil {
					test.Fatalf("reading %s: %v", end.Filename, readError)
				}
				if end.Offset != len(contents) {
					test.Errorf("%s's finding ends at offset %d, and the file ends at %d", run.name, end.Offset,
						len(contents))
				}
			}
		}
		if reported != 1 {
			test.Errorf("%s reported %d findings, where it has one file over the limit", run.name, reported)
		}
	}
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
