package consistency_no_warning_comment

import (
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// A comment group that spells none of the words can hold nothing to report, and nearly every group in a tree is that,
// so judgeGroup must return before it gathers the group's comments and texts. The group has capitals in a row, the
// words in lowercase and mixed case, a directive and a block comment, so the gate is seen turning away a group by its
// words and not by its capitals or its shape.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestJudgeGroupAllocatesNothingWithoutAWarningWord(test *testing.T) {
	const source = `package quiet

// Quiet reads its settings over HTTP as JSON, and a todo list, a Fixme branch
// and xxx in a name are prose.
//
//go:generate echo nothing
/* a block comment in the same group, with an ID */
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
	// A finding would reach the pass's Report and the nil line table, so a group that reported would fail by panicking.
	pass := &analysis.Pass{}
	allocations := testing.AllocsPerRun(10, func() { judgeGroup(pass, nil, nil, group) })
	if allocations != 0 {
		test.Errorf("judgeGroup allocated %.0f times over a group with none of the words: it gathered the group the "+
			"gate skips", allocations)
	}
}
