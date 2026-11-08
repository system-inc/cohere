package unused_code

import (
	"cmp"
	"fmt"
	"go/token"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
)

// The original and the port over the copy of the original's testdata, test variants included, and their findings
// must be the same multiset of file, line, column and message. The original reports nothing itself, so its findings
// are originalReport's, reported as the registry's U1000 reported them before the port took its place. Each side's
// count is logged, since two empty sides would match and prove nothing, and the test fails if the original finds
// nothing at all. The shared parity harness (#dcgn32d) takes this over once it lands.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	actions, runError := fixtureRun()
	if runError != nil {
		test.Fatal(runError)
	}
	original := findingsOf(actions.originalReported)
	port := findingsOf(actions.port)
	compare := func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(original, compare)
	slices.SortFunc(port, compare)

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

// findingsOf is every finding actions reported.
func findingsOf(actions []*checker.Action) []finding {
	findings := []finding{}
	for _, action := range actions {
		for _, diagnostic := range action.Diagnostics {
			findings = append(findings, findingAt(action.Package.Fset.Position(diagnostic.Pos), diagnostic.Message))
		}
	}
	return findings
}

// finding is one finding as the differential compares it.
type finding struct {
	file    string
	line    int
	column  int
	message string
}

// findingAt is a finding at position, its file named relative to the fixtures' tree, since two packages there hold a
// file of the same name.
func findingAt(position token.Position, message string) finding {
	file := position.Filename
	relative, relativeError := filepath.Rel(filepath.Join(analysistest.TestData(), "src"), file)
	if relativeError == nil {
		file = relative
	}
	return finding{file: file, line: position.Line, column: position.Column, message: message}
}

// described is found as a finding line prints it.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d: %s", found.file, found.line, found.column, found.message)
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
