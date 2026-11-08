package string_int_conv

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/stringintconv"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for stringintconv, copied unchanged into testdata/src with their golden files and run as
// x/tools' test runs them, with no flags: a and typeparams for their wants, and fix with its suggested fixes too.
// Every `// want` the original expects, the port reports at the same line with the same message, analysistest fails
// on a finding no want expects as much as on a want nothing found, so the port adds nothing there either, and every
// fix the port offers in fix, applied, turns each file into its golden one.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, "a", "typeparams")
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, analysistest.TestData(), Analyzer, "fix")
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the judgment and of both fixes, the import each fix adds in every layout of a file's
// imports among them, and the shapes most likely to differ. Their findings must be the same multiset of file, line,
// column, end, message and fix, edit for edit, each edit as its offsets and new text, which a golden file, gofmt'd
// before it is compared, would not hold to the character. x/tools' typeparams holds one fix the port withholds, so it
// is held to the original in the differences tier, as strictly but for that one record. Each side's count is logged,
// since two empty sides would match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "a", "fix", "parity", "parity/fresh", "example.com/library")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(original, port) {
		test.Errorf("the original found %s, and the port did not", described(missing))
	}
	for _, extra := range difference(port, original) {
		test.Errorf("the port found %s, and the original did not", described(extra))
	}
}

// withholdingPattern is a record in the differences fixture: the message of a fix the original offers on that line
// and the port withholds, after the comment's `// port withholds: ` opening.
var withholdingPattern = regexp.MustCompile(`^withholds: (.+?)\s*$`)

// upstreamWithholdings is each fix the port withholds in x/tools' own fixtures, keyed by file and line, which their
// files, copied byte for byte, cannot carry as a comment.
var upstreamWithholdings = map[string]string{
	// `AllString(p)`: fmt.Sprint(p) is a string, where the conversion it would replace is an AllString, a type
	// parameter whose one term is string.
	"typeparams.go:34": "Format the number as a decimal",
}

// Over the differences fixture and x/tools' typeparams, the two make the same findings, at the same places in the
// same words, and offer the same fixes, edit for edit, but where a record says the port withholds one: a fix the
// original offers that would be wrong applied. The test fails on a finding one side alone makes, on a fix one side
// alone offers that no record names, on a record whose line offers no such fix, and on a record with no reason on the
// line above it. Each side's counts are logged, and the test fails if the port withholds nothing.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	withheld := recordedWithholdings(test)
	for place, message := range upstreamWithholdings {
		withheld[place] = append(withheld[place], message)
	}
	original, port := offeredOf(test, "differences", "typeparams")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	unmatched := map[string][]string{}
	for place, messages := range withheld {
		unmatched[place] = slices.Clone(messages)
	}
	withheldCount := 0
	for found, originalFixes := range original {
		portFixes, portFound := port[found]
		if !portFound {
			test.Errorf("the original found %s, and the port did not", described(found))
			continue
		}
		place := fmt.Sprintf("%s:%d", found.file, found.line)
		// The port's fixes are the original's, in order, less each one recorded as withheld on the finding's line.
		expected := []suggestion{}
		for _, fix := range originalFixes {
			if index := slices.Index(unmatched[place], fix.message); index >= 0 {
				unmatched[place] = slices.Delete(unmatched[place], index, index+1)
				withheldCount++
				continue
			}
			expected = append(expected, fix)
		}
		if !slices.Equal(expected, portFixes) {
			test.Errorf("at %s the port offers %v, where the original offers %v and the records withhold %v",
				described(found), portFixes, originalFixes, withheld[place])
		}
	}
	for found := range port {
		if _, originalFound := original[found]; !originalFound {
			test.Errorf("the port found %s, and the original did not", described(found))
		}
	}
	for place, messages := range unmatched {
		for _, message := range messages {
			test.Errorf("%s records %q withheld, and the original offers no such fix there", place, message)
		}
	}
	test.Logf("the port withholds %d of the original's fixes", withheldCount)
	if withheldCount == 0 {
		test.Fatal("the port withholds nothing in the differences fixture, so it proves nothing")
	}
}

// recordedWithholdings is every record in the differences fixture, keyed by file and line, each the message of one
// fix withheld there. A `// port ` comment that is no withholding, or a record with no comment on the line above it,
// which says why, fails the test.
func recordedWithholdings(test *testing.T) map[string][]string {
	test.Helper()
	path := filepath.Join(analysistest.TestData(), "src", "differences", "differences.go")
	content, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	withheld := map[string][]string{}
	lines := strings.Split(string(content), "\n")
	for index, text := range lines {
		for _, comment := range strings.Split(text, "// port ")[1:] {
			match := withholdingPattern.FindStringSubmatch(comment)
			if match == nil {
				test.Fatalf("differences.go:%d holds a comment the test cannot read: %q", index+1, comment)
			}
			if index == 0 || !strings.HasPrefix(strings.TrimSpace(lines[index-1]), "//") {
				test.Fatalf("differences.go:%d withholds %q with no reason on the line above", index+1, match[1])
			}
			place := fmt.Sprintf("differences.go:%d", index+1)
			withheld[place] = append(withheld[place], match[1])
		}
	}
	return withheld
}

// suggestion is one suggested fix as the differences tier compares it: its message, and the fix described whole, its
// edits as describedFixes describes them.
type suggestion struct {
	message   string
	described string
}

// offeredOf loads the fixture packages patterns name, runs x/tools' stringintconv and the port over them, and returns
// each one's findings, keyed by place and message, each with its fixes apart. A place and message found twice by one
// side fails the test, since the differences tier pairs the two sides' findings by them.
func offeredOf(
	test *testing.T, patterns ...string,
) (original map[finding][]suggestion, port map[finding][]suggestion) {
	test.Helper()
	original, port = map[finding][]suggestion{}, map[finding][]suggestion{}
	for _, reported := range diagnosticsOf(test, patterns...) {
		found := located(reported.action, reported.diagnostic)
		fixes := []suggestion{}
		for _, fix := range reported.diagnostic.SuggestedFixes {
			alone := analysis.Diagnostic{SuggestedFixes: []analysis.SuggestedFix{fix}}
			fixes = append(fixes, suggestion{message: fix.Message, described: describedFixes(reported.action, alone)})
		}
		side := port
		if reported.action.Analyzer == stringintconv.Analyzer {
			side = original
		}
		if _, repeated := side[found]; repeated {
			test.Fatalf("%s found %s twice", reported.action.Analyzer.Name, described(found))
		}
		side[found] = fixes
	}
	return original, port
}

// finding is one finding as the differential compares it, its suggested fixes included.
type finding struct {
	file    string
	line    int
	column  int
	end     string
	message string
	fixes   string
}

// described is found as a finding line prints it, followed by its end and its fixes.
func described(found finding) string {
	return fmt.Sprintf(
		"%s:%d:%d (to %s): %s%s", found.file, found.line, found.column, found.end, found.message, found.fixes,
	)
}

// findingsOf loads the fixture packages patterns name, runs x/tools' stringintconv and the port over them in one
// checker, and returns each one's findings, fixes included, sorted.
func findingsOf(test *testing.T, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	for _, reported := range diagnosticsOf(test, patterns...) {
		found := located(reported.action, reported.diagnostic)
		found.fixes = describedFixes(reported.action, reported.diagnostic)
		switch reported.action.Analyzer {
		case stringintconv.Analyzer:
			original = append(original, found)
		case Analyzer:
			port = append(port, found)
		}
	}
	compare := func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(original, compare)
	slices.SortFunc(port, compare)
	return original, port
}

// reportedDiagnostic is one diagnostic and the action, the original's or the port's on one package, that reported it.
type reportedDiagnostic struct {
	action     *checker.Action
	diagnostic analysis.Diagnostic
}

// diagnosticsOf loads the fixture packages patterns name, runs x/tools' stringintconv and the port over them in one
// checker, and returns every diagnostic either reports, with the action that reported it.
func diagnosticsOf(test *testing.T, patterns ...string) []reportedDiagnostic {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	// Every fixture must type-check, or the checker's guesses at a broken conversion would stand in for the shapes it
	// holds.
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("%s does not type-check: %v", root.PkgPath, root.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{stringintconv.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	reported := []reportedDiagnostic{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			reported = append(reported, reportedDiagnostic{action: action, diagnostic: diagnostic})
		}
	}
	return reported
}

// located is diagnostic's place and message, as action's package positions them, with no fixes.
func located(action *checker.Action, diagnostic analysis.Diagnostic) finding {
	position := action.Package.Fset.Position(diagnostic.Pos)
	end := action.Package.Fset.Position(diagnostic.End)
	return finding{
		file:    filepath.Base(position.Filename),
		line:    position.Line,
		column:  position.Column,
		end:     fmt.Sprintf("%d:%d", end.Line, end.Column),
		message: diagnostic.Message,
	}
}

// describedFixes is every suggested fix of diagnostic, in order, as its message and each edit's file, offsets and new
// text, in the order the fix lists them, so two findings match only where they offer the same edits to the character.
// It is empty for a finding with no fix.
func describedFixes(action *checker.Action, diagnostic analysis.Diagnostic) string {
	var fixes strings.Builder
	for _, fix := range diagnostic.SuggestedFixes {
		fmt.Fprintf(&fixes, " [fix %q:", fix.Message)
		for _, edit := range fix.TextEdits {
			start := action.Package.Fset.Position(edit.Pos)
			end := action.Package.Fset.Position(edit.End)
			fmt.Fprintf(
				&fixes, " %s@%d-%d %q", filepath.Base(start.Filename), start.Offset, end.Offset, edit.NewText,
			)
		}
		fixes.WriteString("]")
	}
	return fixes.String()
}

// difference is what of from is not in without, counting repeats, so a finding reported twice by one side and once by
// the other is a difference.
func difference(from []finding, without []finding) []finding {
	remaining := map[finding]int{}
	for _, found := range without {
		remaining[found]++
	}
	missing := []finding{}
	for _, found := range from {
		if remaining[found] > 0 {
			remaining[found]--
			continue
		}
		missing = append(missing, found)
	}
	return missing
}

// errorsOnly hands analysistest a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
