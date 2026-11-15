package unread_assigned_value

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// allocationSource holds functions Check judges and finds nothing in, so it reports through no context: one with no
// assignment, ones whose values are read directly, through a phi and in a loop, and one whose switch has no tag.
const allocationSource = `package allocations

func none(value int) int { return value * 2 }

func assigns(value int) int {
	doubled := value * 2
	tripled := doubled + value
	return tripled
}

func joins(value int, isLarger bool) int {
	result := value + 1
	if isLarger {
		result = value + 2
	}
	return result
}

func loops(count int) int {
	total := 0
	for index := 0; index < count; index++ {
		total += index
	}
	return total
}

func untagged(value int) int {
	switch {
	case value > 1:
		return 2
	}
	return 3
}
`

// TestCheckAllocatesOnlyItsWalkerWithoutATaggedSwitch holds Check, for a function without a tagged switch, to the one
// walker both its walks share, where a map of switch tags, a closure for each ast.Inspect and one for the use test
// cost four.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestCheckAllocatesOnlyItsWalkerWithoutATaggedSwitch(test *testing.T) {
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "allocations.go", allocationSource, 0)
	if parseError != nil {
		test.Fatalf("parsing the source: %v", parseError)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Scopes:     map[ast.Node]*types.Scope{},
	}
	packageTypes, checkError := (&types.Config{}).Check("allocations", fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("type-checking the source: %v", checkError)
	}
	built := build_intermediate_representation.NewPackage(fileSet, packageTypes, []*ast.File{file}, info,
		func(*types.Func) bool { return false }, true)
	built.Build()
	notGenerated := func(token.Pos) (generated.Generator, bool) { return 0, false }
	for _, name := range []string{"none", "assigns", "joins", "loops", "untagged"} {
		function := built.Function(name)
		if function == nil {
			test.Fatalf("the source has no function %s", name)
		}
		// Not parallel: each function counts the process's allocations, as the test does.
		allocations := testing.AllocsPerRun(100, func() { Check(nil, function, notGenerated) })
		if allocations != 1 {
			test.Errorf("Check of %s allocates %v times, want 1, its walker", name, allocations)
		}
	}
}
