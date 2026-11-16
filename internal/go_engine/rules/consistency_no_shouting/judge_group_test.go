package consistency_no_shouting

import (
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// A comment group with no two capitals in a row in any comment can hold nothing to report, and most of a tree's groups
// are that, so judgeGroup must return before it joins the group's bodies, which costs a builder and a slice of spans.
// The group mixes // lines, a directive and a block comment, so each way a body is cut is one the gate skips.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestJudgeGroupAllocatesNothingWithoutTwoCapitalsInARow(test *testing.T) {
	const source = `package quiet

// Quiet reads a comment in a normal voice, and a Capital here and there
// is only the start of a word.
//
//go:generate echo nothing
/* a block comment on the same group */
var Quiet = 1
`
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "quiet.go", source, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing the case: %v", parseError)
	}
	if len(file.Comments) != 1 {
		test.Fatalf("the case holds %d comment groups, and it is meant to hold one", len(file.Comments))
	}
	group := file.Comments[0]
	// A finding would reach the pass's Report, which is nil here, so a group that reported would fail by panicking.
	pass := &analysis.Pass{}
	allocations := testing.AllocsPerRun(10, func() { judgeGroup(pass, group, nil) })
	if allocations != 0 {
		test.Errorf("judgeGroup allocated %.0f times over a group with no two capitals in a row: it joined the "+
			"group's bodies the gate skips", allocations)
	}
}
