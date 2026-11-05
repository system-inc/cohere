package consistency_no_dash_punctuation_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_dash_punctuation"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_dash_punctuation.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-dash-punctuation"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixture carries a want for every dash written as itself: in a doc comment, a line comment, a block
// comment on one line and across lines, an interpreted string, and a raw string on one line and across lines. Its
// .golden file is the source with every offered fix applied, so a fix that writes the wrong escape, or one offered
// for a raw string or a comment, fails even when the finding is right. analysistest fails on a finding no want
// expects as much as on a want nothing found, so the allowed and lookalike fixtures prove the rule stays quiet on a
// dash spelled by \u, \U, \x and octal escapes, a hyphen-minus, the minus sign, the Unicode hyphen and a rune
// literal. Each finding's span is then checked against the dash it names.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	results := analysistest.RunWithSuggestedFixes(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_dash_punctuation.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
	)
	checkReportedSpans(test, results)
}

// The fix rewrites a dash as its escape on the promise that the value is the same bytes, so the program reads the
// same string. The flagged fixture is analyzed, every fix the rule offers is applied to its source, and every string
// literal is unquoted beside its twin in the original: the two must be equal, and at least one pair must differ in
// source, so the test cannot pass by comparing nothing rewritten. The golden file pins the fixes' text; this pins
// their meaning, from the rule's own edits.
func TestFixKeepsEveryValue(test *testing.T) {
	test.Parallel()
	results := analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_dash_punctuation.Analyzer,
		"flagged",
	)
	path := filepath.Join(analysistest.TestData(), "src", "flagged", "flagged.go")
	source, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	// Each edit replaces one dash, so no two overlap, and applying them from the end of the file backwards keeps
	// every earlier offset where the analysis found it.
	edits := []analysis.TextEdit{}
	var fileSet *token.FileSet
	for _, result := range results {
		fileSet = result.Pass.Fset
		for _, diagnostic := range result.Diagnostics {
			for _, fix := range diagnostic.SuggestedFixes {
				edits = append(edits, fix.TextEdits...)
			}
		}
	}
	if fileSet == nil {
		test.Fatal("the flagged fixture was not analyzed")
	}
	slices.SortFunc(edits, func(left, right analysis.TextEdit) int { return int(right.Pos - left.Pos) })
	fixedSource := slices.Clone(source)
	for _, edit := range edits {
		start := fileSet.PositionFor(edit.Pos, false).Offset
		end := fileSet.PositionFor(edit.End, false).Offset
		fixedSource = slices.Concat(fixedSource[:start], edit.NewText, fixedSource[end:])
	}
	originals := stringLiterals(test, source)
	fixed := stringLiterals(test, fixedSource)
	if len(originals) != len(fixed) {
		test.Fatalf("the fix changed how many string literals there are, from %d to %d", len(originals), len(fixed))
	}
	rewritten := 0
	for index, original := range originals {
		if original == fixed[index] {
			continue
		}
		rewritten++
		originalValue, originalError := strconv.Unquote(original)
		if originalError != nil {
			test.Fatalf("unquoting %s: %v", original, originalError)
		}
		fixedValue, fixedError := strconv.Unquote(fixed[index])
		if fixedError != nil {
			test.Fatalf("unquoting %s: %v", fixed[index], fixedError)
		}
		if originalValue != fixedValue {
			test.Errorf("the fix rewrote %s as %s, which unquotes to %q, not %q", original, fixed[index], fixedValue,
				originalValue)
		}
	}
	if rewritten == 0 {
		test.Error("no string literal differs between the fixture and its golden file, so no fix was checked")
	}
}

// stringLiterals is the source of every string literal in source, in the order it holds them.
func stringLiterals(test *testing.T, source []byte) []string {
	test.Helper()
	file, parseError := parser.ParseFile(token.NewFileSet(), "flagged.go", source, 0)
	if parseError != nil {
		test.Fatal(parseError)
	}
	literals := []string{}
	ast.Inspect(file, func(node ast.Node) bool {
		if literal, isLiteral := node.(*ast.BasicLit); isLiteral && literal.Kind == token.STRING {
			literals = append(literals, literal.Value)
		}
		return true
	})
	return literals
}

// Go's scanner drops every carriage return from a block comment's and a raw string's text, so in a file with CRLF
// line endings a dash on a later line sits further into the file than its offset into that text says. The flagged
// fixture is run again with CRLF endings to prove each finding still lands on the dash itself. The copy is written
// at test time because the repository's .gitattributes keeps every committed file LF.
func TestAnalyzerWithCarriageReturns(test *testing.T) {
	test.Parallel()
	source, readError := os.ReadFile(filepath.Join(analysistest.TestData(), "src", "flagged", "flagged.go"))
	if readError != nil {
		test.Fatal(readError)
	}
	directory := test.TempDir()
	packageDirectory := filepath.Join(directory, "src", "flagged")
	if makeError := os.MkdirAll(packageDirectory, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	carriageReturnSource := bytes.ReplaceAll(source, []byte("\n"), []byte("\r\n"))
	writeError := os.WriteFile(filepath.Join(packageDirectory, "flagged.go"), carriageReturnSource, 0o644)
	if writeError != nil {
		test.Fatal(writeError)
	}
	results := analysistest.Run(errorsOnly{test}, directory, consistency_no_dash_punctuation.Analyzer, "flagged")
	checkReportedSpans(test, results)
}

// checkReportedSpans reads each finding's own bytes back from its file, since a want matches only a line: every
// finding must be reported exactly at the dash it names, and a fix must replace that dash and nothing else.
func checkReportedSpans(test *testing.T, results []*analysistest.Result) {
	test.Helper()
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			// The file's own offsets, not ones a //line directive would give, since the bytes are read from it.
			start := result.Pass.Fset.PositionFor(diagnostic.Pos, false)
			end := result.Pass.Fset.PositionFor(diagnostic.End, false)
			source, readError := os.ReadFile(start.Filename)
			if readError != nil {
				test.Fatal(readError)
			}
			reported := string(source[start.Offset:end.Offset])
			character, size := utf8.DecodeRuneInString(reported)
			if (character != '\u2014' && character != '\u2013') || size != len(reported) {
				test.Errorf("%s: a dash reported over %q, not the dash alone", start, reported)
			}
			for _, fix := range diagnostic.SuggestedFixes {
				for _, edit := range fix.TextEdits {
					if edit.Pos != diagnostic.Pos || edit.End != diagnostic.End {
						test.Errorf("%s: a fix edits beyond the dash it was offered for", start)
					}
				}
			}
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
