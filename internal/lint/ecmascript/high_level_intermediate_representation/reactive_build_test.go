package high_level_intermediate_representation

import (
	"testing"

	"github.com/system-inc/cohere/modules/static_single_assignment"
)

// TestBuildReactiveFunctionShapes asserts the tree each control-flow construct converts to.
//
// One source per construct, chosen so the four ways a block can leave the graph are each covered
// once: a branch with a join, a ternary and a logical (both value terminals), and a loop with a
// back edge.
//
// The assertion is instruction conservation rather than a printed shape. A golden string would fail
// on every cosmetic change to the printer and would not notice the one failure that matters, which
// is an instruction entering the graph and not arriving in the tree. Conservation is the property
// upstream's own conversion has and the property every downstream pass depends on: a pass that reads
// the tree to decide what a scope depends on cannot see an instruction the converter dropped.
func TestBuildReactiveFunctionShapes(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name           string
		valueTerminals int
		// A reconstructed value terminal introduces one reactive instruction for the `HIR` terminal
		// itself. Its test and arms remain nested values, so this is one structural instruction
		// beyond the graph's instruction table rather than a duplicated block.
		reactiveExtra int
		source        string
	}{
		{name: "branch with join", source: `function f(a) { if (a) { return 1; } return 2; }`},
		{
			name:   "loop with back edge",
			source: `function f(xs) { let t = 0; for (const x of xs) { t = t + x; } return t; }`,
		},
		{name: "ternary", valueTerminals: 1, reactiveExtra: 1,
			source: `function f(a) { const x = a ? 1 : 2; return x; }`},
		{name: "logical", valueTerminals: 1, reactiveExtra: 1,
			source: `function f(a) { const x = a && a.b; return x; }`},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			function, _ := rangesFor(test, testCase.source)
			if function == nil {
				test.Fatal("the source did not lower to a function, so every assertion below would " +
					"pass vacuously")
			}

			graphInstructions := 0
			valueTerminals := 0
			for _, block := range function.Blocks {
				if block == nil {
					continue
				}
				graphInstructions += len(block.Instructions)
				switch block.Terminal.(type) {
				case *Logical, *Ternary, *Optional, *Sequence:
					valueTerminals++
				}
			}
			if graphInstructions == 0 {
				test.Fatal("the graph holds no instructions, so conservation is trivially satisfied " +
					"and this case measures nothing")
			}

			// The case's own claim about its lowering is asserted, not assumed. If a change to
			// lowering stops emitting a ternary as a value terminal, this test would otherwise
			// quietly start applying the wrong assertion below and still pass.
			if valueTerminals != testCase.valueTerminals {
				test.Fatalf("this case claims valueTerminals=%d but its lowering says %d; the "+
					"claim decides which assertion applies, so a stale one hides a real change",
					testCase.valueTerminals, valueTerminals)
			}

			tree, result := BuildReactiveFunction(function)
			if tree == nil {
				test.Fatal("BuildReactiveFunction returned no tree for a function that lowered")
			}

			if result.DoubleEmitted != 0 {
				test.Errorf("%d block(s) emitted twice; a block reached from two places must be "+
					"scheduled by its parent and broken to, not walked again",
					result.DoubleEmitted)
			}
			if result.UnmatchedGotos != 0 {
				test.Errorf("%d goto(s) found no enclosing construct to break to, so the tree names a "+
					"target that is not on the control-flow stack", result.UnmatchedGotos)
			}

			wantInstructions := graphInstructions + testCase.reactiveExtra
			if result.Instructions != wantInstructions {
				test.Errorf("reactive tree holds %d instructions, want %d from %d graph instructions "+
					"plus %d reconstructed terminal instruction(s)", result.Instructions,
					wantInstructions, graphInstructions, testCase.reactiveExtra)
			}
		})
	}
}

func TestBuildReactiveFunctionOptionalValue(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name   string
		source string
		want   int
	}{
		{name: "single link", source: `function f(a) { return a?.b; }`, want: 1},
		{name: "nested links", source: `function f(a) { return a?.b?.c; }`, want: 2},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			function, _ := rangesFor(test, testCase.source)
			if function == nil {
				test.Fatal("source did not lower")
			}

			graphOptionals := 0
			joinPhis := map[static_single_assignment.IdentifierId]bool{}
			for _, block := range function.Blocks {
				if terminal, ok := block.Terminal.(*Optional); ok {
					graphOptionals++
					if join, ok := function.Block(terminal.Fallthrough); ok {
						for _, phi := range join.Phis {
							joinPhis[phi.Place.Identifier] = true
						}
					}
				}
			}
			if graphOptionals != testCase.want {
				test.Fatalf("graph holds %d Optional terminals, want %d; the test no longer reaches "+
					"the reconstruction path", graphOptionals, testCase.want)
			}

			tree, result := BuildReactiveFunction(function)
			if tree == nil {
				test.Fatal("BuildReactiveFunction returned no tree")
			}
			if result.DoubleEmitted != 0 {
				test.Fatalf("reconstructing the chain emitted %d block(s) twice", result.DoubleEmitted)
			}

			values, statementIfs := 0, 0
			defined := map[static_single_assignment.IdentifierId]bool{}
			VisitReactiveFunction(tree, ReactiveVisitor{
				Instruction: func(instruction *ReactiveInstruction, traverse func()) {
					if instruction.LValue != nil {
						defined[instruction.LValue.Identifier] = true
					}
					traverse()
				},
				Value: func(_ static_single_assignment.EvaluationOrder, value ReactiveValue, traverse func()) {
					if optional, ok := value.(*ReactiveOptionalValue); ok {
						values++
						if _, ok := optional.Value.(*ReactiveSequenceValue); !ok {
							test.Errorf("optional value wraps %T, want the test/consequent sequence", optional.Value)
						}
					}
					traverse()
				},
				Terminal: func(statement *ReactiveTerminalStatement, traverse func()) {
					if _, ok := statement.Terminal.(*ReactiveIf); ok {
						statementIfs++
					}
					traverse()
				},
			})
			if values != testCase.want {
				test.Errorf("tree holds %d ReactiveOptionalValue nodes, want %d", values, testCase.want)
			}
			if statementIfs != 0 {
				test.Errorf("optional expression became %d statement-level if(s)", statementIfs)
			}
			for identifier := range joinPhis {
				if !defined[identifier] {
					test.Errorf("optional join phi %d is read after the expression but never defined in the tree",
						identifier)
				}
			}
		})
	}
}

func TestBuildReactiveFunctionTernaryValue(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name   string
		source string
		want   int
	}{
		{name: "single conditional", source: `function f(a, b, c) { return a ? b : c; }`, want: 1},
		{name: "nested conditional", source: `function f(a, b, c, d, e) { return a ? (b ? c : d) : e; }`, want: 2},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			function, _ := rangesFor(test, testCase.source)
			if function == nil {
				test.Fatal("source did not lower")
			}

			graphTernaries := 0
			joinPhis := map[static_single_assignment.IdentifierId]bool{}
			for _, block := range function.Blocks {
				if terminal, ok := block.Terminal.(*Ternary); ok {
					graphTernaries++
					if join, ok := function.Block(terminal.Fallthrough); ok {
						for _, phi := range join.Phis {
							joinPhis[phi.Place.Identifier] = true
						}
					}
				}
			}
			if graphTernaries != testCase.want {
				test.Fatalf("graph holds %d Ternary terminals, want %d; the test no longer reaches "+
					"the reconstruction path", graphTernaries, testCase.want)
			}
			tree, result := BuildReactiveFunction(function)
			if tree == nil {
				test.Fatal("BuildReactiveFunction returned no tree")
			}
			if result.DoubleEmitted != 0 {
				test.Fatalf("reconstructing the conditional emitted %d block(s) twice", result.DoubleEmitted)
			}

			values, statementIfs := 0, 0
			defined := map[static_single_assignment.IdentifierId]bool{}
			VisitReactiveFunction(tree, ReactiveVisitor{
				Instruction: func(instruction *ReactiveInstruction, traverse func()) {
					if instruction.LValue != nil {
						defined[instruction.LValue.Identifier] = true
					}
					traverse()
				},
				Value: func(_ static_single_assignment.EvaluationOrder, value ReactiveValue, traverse func()) {
					if conditional, ok := value.(*ReactiveTernaryValue); ok {
						values++
						if conditional.Test == nil || conditional.Consequent == nil || conditional.Alternate == nil {
							test.Error("conditional value does not contain its test and both arms")
						}
					}
					traverse()
				},
				Terminal: func(statement *ReactiveTerminalStatement, traverse func()) {
					if _, ok := statement.Terminal.(*ReactiveIf); ok {
						statementIfs++
					}
					traverse()
				},
			})
			if values != testCase.want {
				test.Errorf("tree holds %d ReactiveTernaryValue nodes, want %d", values, testCase.want)
			}
			if statementIfs != 0 {
				test.Errorf("conditional expression became %d statement-level if(s)", statementIfs)
			}
			for identifier := range joinPhis {
				if !defined[identifier] {
					test.Errorf("conditional join phi %d is read after the expression but never defined in the tree",
						identifier)
				}
			}
		})
	}
}

// TestBuildReactiveFunctionLogicalKeepsRightPrefix is the preservation-memoization golden that
// exposed the wrong logical arm. The `?? []` right block contains both ArrayExpression and a final
// LoadLocal; rebuilding only the final value drops the allocation, which in turn removes the scope
// declaration that the validator needs.
func TestBuildReactiveFunctionLogicalKeepsRightPrefix(test *testing.T) {
	test.Parallel()

	function, _ := rangesFor(test, `
		function Component(props) {
			const data = useMemo(() => {
				return props.items.edges.nodes ?? [];
			}, [props.items?.edges?.nodes]);
			return data;
		}
	`)
	if len(function.Functions) != 1 {
		test.Fatalf("component holds %d nested functions, want the useMemo callback", len(function.Functions))
	}
	callback := function.Functions[0]

	graphArrays, graphLogicals := 0, 0
	for _, block := range callback.Blocks {
		if _, ok := block.Terminal.(*Logical); ok {
			graphLogicals++
		}
		for _, instructionId := range block.Instructions {
			if _, ok := callback.Instructions[instructionId].Value.(*ArrayExpression); ok {
				graphArrays++
			}
		}
	}
	if graphArrays != 1 || graphLogicals != 1 {
		test.Fatalf("callback graph has %d arrays and %d logical terminals, want one of each; the "+
			"fixture no longer pins the lost-prefix shape", graphArrays, graphLogicals)
	}

	tree, result := BuildReactiveFunction(callback)
	if tree == nil {
		test.Fatal("BuildReactiveFunction returned no callback tree")
	}
	if result.DoubleEmitted != 0 {
		test.Fatalf("reconstructing the logical emitted %d block(s) twice", result.DoubleEmitted)
	}

	treeArrays, treeLogicals := 0, 0
	VisitReactiveFunction(tree, ReactiveVisitor{
		Value: func(_ static_single_assignment.EvaluationOrder, value ReactiveValue, traverse func()) {
			switch shape := value.(type) {
			case *ReactiveLogicalValue:
				treeLogicals++
			case *ReactiveInstructionValue:
				if _, ok := shape.Value.(*ArrayExpression); ok {
					treeArrays++
				}
			}
			traverse()
		},
	})
	if treeLogicals != graphLogicals {
		test.Errorf("tree holds %d logical values, want %d", treeLogicals, graphLogicals)
	}
	if treeArrays != graphArrays {
		test.Errorf("tree holds %d array allocations, want all %d from the graph", treeArrays, graphArrays)
	}
}

// TestReactiveFunctionGapsAreDeclared pins the gap list so closing one is a visible event.
//
// `ReactiveFunctionGaps` exists to be asserted on, for the reason its own comment gives: a gap that
// closes silently is an improvement nobody can point at, and a gap that opens silently is a
// regression nobody can either.
func TestReactiveFunctionGapsAreDeclared(test *testing.T) {
	test.Parallel()

	gaps := ReactiveFunctionGaps()
	if len(gaps) != 4 {
		test.Fatalf("expected 4 declared gaps, found %d; a gap was added or closed without this "+
			"test and its consumers being updated", len(gaps))
	}

	seen := map[ReactiveFunctionGap]bool{}
	for _, gap := range gaps {
		if seen[gap] {
			test.Errorf("gap %v is listed twice", gap)
		}
		seen[gap] = true
	}
	for _, required := range []ReactiveFunctionGap{
		ReactiveFunctionGapUnprunedLabels,
		ReactiveFunctionGapPrunedScopes,
		ReactiveFunctionGapUnbuiltTerminals,
		ReactiveFunctionGapValueExpressions,
	} {
		if !seen[required] {
			test.Errorf("gap %v is declared as a constant but not returned by ReactiveFunctionGaps, "+
				"so nothing asserts on it", required)
		}
	}
}
