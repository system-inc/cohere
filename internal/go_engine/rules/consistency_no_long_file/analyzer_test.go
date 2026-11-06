package consistency_no_long_file_test

import (
	"os"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_file"
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

// With max set to 3,000, a file of 2,100 lines, past the house limit, is clean and its neighbour of 3,001 is flagged
// at its 3,001st, so a looser limit is taken and moves the finding with it as a stricter one does. Whether the
// settings give a reason for it is the engine's check, not this rule's.
func TestAnalyzerWithALooserLimit(test *testing.T) {
	test.Parallel()
	analyzer := consistency_no_long_file.NewAnalyzer()
	if setError := analyzer.Flags.Set("max", "3000"); setError != nil {
		test.Fatal(setError)
	}
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), analyzer, "looser")
}

// The max option takes a looser limit as it takes a stricter one, and its value says by its own type which it is: a
// limit above the house's 2,000 loosens and names the house limit, and the house limit itself and anything below it
// does not, so the engine asks for a departure for 2,001 and 3,000 alone.
func TestLimitSaysWhetherItLoosens(test *testing.T) {
	test.Parallel()
	for text, wantLoosens := range map[string]bool{"1": false, "1500": false, "2000": false, "2001": true,
		"3000": true} {
		analyzer := consistency_no_long_file.NewAnalyzer()
		if setError := analyzer.Flags.Set("max", text); setError != nil {
			test.Fatalf("setting the limit to %s was refused: %v", text, setError)
		}
		value := analyzer.Flags.Lookup("max").Value
		if read := value.String(); read != text {
			test.Errorf("the limit set to %s reads back as %s", text, read)
		}
		loosening, isLoosening := value.(rule.Loosening)
		if !isLoosening {
			test.Fatalf("the max option's value is %T, which does not say whether it loosens", value)
		}
		if loosening.Loosens() != wantLoosens {
			test.Errorf("a limit of %s loosens %t, want %t", text, loosening.Loosens(), wantLoosens)
		}
		if house := loosening.HouseValue(); house != "2000" {
			test.Errorf("the house value reads %s, want 2000", house)
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
