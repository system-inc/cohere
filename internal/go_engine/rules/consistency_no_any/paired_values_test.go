package consistency_no_any

import (
	"flag"
	"go/ast"
	"testing"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// pairedSource defines three names from three values and two from one call's two results, the two ways valuesOf
// pairs values with their receivers.
const pairedSource = "package flows\n\n" +
	"func pair() (int, string) { return 0, \"\" }\n\n" +
	"func run() {\n" +
	"\tleft, right, middle := 1, \"two\", 3.0\n" +
	"\tfirst, second := pair()\n" +
	"\t_, _, _, _, _ = left, right, middle, first, second\n" +
	"}\n"

// valuesOf pairs each statement's values in a buffer the analysis holds, so once the buffer has grown, pairing
// allocates nothing, where a slice made for each statement allocated once for every assignment, var declaration and
// return of a package holding a candidate. The values are the ones a fresh slice would hold, and the slice handed back
// is capped at its length, so a caller appending to it copies out rather than writing into the buffer.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestValuesArePairedWithoutAllocating(test *testing.T) {
	checked := checkedPackageOf(test, pairedSource)
	assignments := []*ast.AssignStmt{}
	ast.Inspect(checked.file, func(node ast.Node) bool {
		if assignment, isAssignment := node.(*ast.AssignStmt); isAssignment && len(assignment.Lhs) != 5 {
			assignments = append(assignments, assignment)
		}
		return true
	})
	if len(assignments) != 2 {
		test.Fatalf("want the two definitions, got %d", len(assignments))
	}
	spread, called := assignments[0], assignments[1]
	context := rule.NewContext(Name, checked.pass, listener.Requires, flag.NewFlagSet(Name, flag.ContinueOnError), nil,
		nil)
	flows := &flowAnalysis{context: context, information: context.TypesInfo, own: context.Package}

	values := flows.valuesOf(spread.Rhs, len(spread.Lhs))
	if len(values) != 3 || cap(values) != 3 {
		test.Fatalf("want three values capped at three, got %d capped at %d", len(values), cap(values))
	}
	for index, value := range values {
		if value.expression != spread.Rhs[index] || value.resultIndex != -1 ||
			value.valueType != context.TypesInfo.TypeOf(spread.Rhs[index]) {
			test.Errorf("value %d is not its own expression's: %+v", index, value)
		}
	}
	results := flows.valuesOf(called.Rhs, len(called.Lhs))
	if len(results) != 2 || cap(results) != 2 {
		test.Fatalf("want two results capped at two, got %d capped at %d", len(results), cap(results))
	}
	for index, want := range []types.Type{types.Typ[types.Int], types.Typ[types.String]} {
		if results[index].expression != called.Rhs[0] || results[index].resultIndex != index ||
			!types.Identical(results[index].valueType, want) {
			test.Errorf("result %d is not the call's: %+v", index, results[index])
		}
	}
	if unpaired := flows.valuesOf(called.Rhs, 3); unpaired != nil {
		test.Errorf("one call of two results paired with three receivers, got %d values", len(unpaired))
	}

	allocations := testing.AllocsPerRun(20, func() {
		flows.valuesOf(spread.Rhs, len(spread.Lhs))
		flows.valuesOf(called.Rhs, len(called.Lhs))
	})
	if allocations != 0 {
		test.Errorf("pairing two statements' values allocated %.0f times, where the buffer must be reused", allocations)
	}
}
