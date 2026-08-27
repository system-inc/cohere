package hir

import (
	"testing"
)

// TestBuildReactiveFunctionShapes asserts the tree each control-flow construct converts to.
//
// One source per construct, chosen so the four ways a block can leave the graph are each covered
// once: a branch with a join, a ternary and a logical (both value terminals), and a loop with a
// back edge.
//
// The assertion is instruction CONSERVATION rather than a printed shape. A golden string would fail
// on every cosmetic change to the printer and would not notice the one failure that matters, which
// is an instruction entering the graph and not arriving in the tree. Conservation is the property
// upstream's own conversion has and the property every downstream pass depends on: a pass that reads
// the tree to decide what a scope depends on cannot see an instruction the converter dropped.
func TestBuildReactiveFunctionShapes(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		valueTerminals int
		// A reconstructed value terminal introduces one reactive instruction for the HIR terminal
		// itself. Its test and arms remain nested values, so this is one structural instruction
		// beyond the graph's instruction table rather than a duplicated block.
		reactiveExtra int
		source        string
	}{
		{name: "branch with join", source: `function f(a) { if (a) { return 1; } return 2; }`},
		{name: "loop with back edge", source: `function f(xs) { let t = 0; for (const x of xs) { t = t + x; } return t; }`},
		{name: "ternary", valueTerminals: 1, reactiveExtra: 1,
			source: `function f(a) { const x = a ? 1 : 2; return x; }`},
		{name: "logical", valueTerminals: 1, reactiveExtra: 1,
			source: `function f(a) { const x = a && a.b; return x; }`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			function, _ := rangesFor(t, testCase.source)
			if function == nil {
				t.Fatal("the source did not lower to a function, so every assertion below would " +
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
				t.Fatal("the graph holds no instructions, so conservation is trivially satisfied " +
					"and this case measures nothing")
			}

			// The case's own claim about its lowering is asserted, not assumed. If a change to
			// lowering stops emitting a ternary as a value terminal, this test would otherwise
			// quietly start applying the wrong assertion below and still pass.
			if valueTerminals != testCase.valueTerminals {
				t.Fatalf("this case claims valueTerminals=%d but its lowering says %d; the "+
					"claim decides which assertion applies, so a stale one hides a real change",
					testCase.valueTerminals, valueTerminals)
			}

			tree, result := BuildReactiveFunction(function)
			if tree == nil {
				t.Fatal("BuildReactiveFunction returned no tree for a function that lowered")
			}

			if result.DoubleEmitted != 0 {
				t.Errorf("%d block(s) emitted twice; a block reached from two places must be "+
					"scheduled by its parent and broken to, not walked again",
					result.DoubleEmitted)
			}
			if result.UnmatchedGotos != 0 {
				t.Errorf("%d goto(s) found no enclosing construct to break to, so the tree names a "+
					"target that is not on the control-flow stack", result.UnmatchedGotos)
			}

			wantInstructions := graphInstructions + testCase.reactiveExtra
			if result.Instructions != wantInstructions {
				t.Errorf("reactive tree holds %d instructions, want %d from %d graph instructions "+
					"plus %d reconstructed terminal instruction(s)", result.Instructions,
					wantInstructions, graphInstructions, testCase.reactiveExtra)
			}
		})
	}
}

func TestBuildReactiveFunctionOptionalValue(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   int
	}{
		{name: "single link", source: `function f(a) { return a?.b; }`, want: 1},
		{name: "nested links", source: `function f(a) { return a?.b?.c; }`, want: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			function, _ := rangesFor(t, testCase.source)
			if function == nil {
				t.Fatal("source did not lower")
			}

			graphOptionals := 0
			joinPhis := map[IdentifierId]bool{}
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
				t.Fatalf("graph holds %d Optional terminals, want %d; the test no longer reaches "+
					"the reconstruction path", graphOptionals, testCase.want)
			}

			tree, result := BuildReactiveFunction(function)
			if tree == nil {
				t.Fatal("BuildReactiveFunction returned no tree")
			}
			if result.DoubleEmitted != 0 {
				t.Fatalf("reconstructing the chain emitted %d block(s) twice", result.DoubleEmitted)
			}

			values, statementIfs := 0, 0
			defined := map[IdentifierId]bool{}
			VisitReactiveFunction(tree, ReactiveVisitor{
				Instruction: func(instruction *ReactiveInstruction, traverse func()) {
					if instruction.LValue != nil {
						defined[instruction.LValue.Identifier] = true
					}
					traverse()
				},
				Value: func(_ EvaluationOrder, value ReactiveValue, traverse func()) {
					if optional, ok := value.(*ReactiveOptionalValue); ok {
						values++
						if _, ok := optional.Value.(*ReactiveSequenceValue); !ok {
							t.Errorf("optional value wraps %T, want the test/consequent sequence", optional.Value)
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
				t.Errorf("tree holds %d ReactiveOptionalValue nodes, want %d", values, testCase.want)
			}
			if statementIfs != 0 {
				t.Errorf("optional expression became %d statement-level if(s)", statementIfs)
			}
			for identifier := range joinPhis {
				if !defined[identifier] {
					t.Errorf("optional join phi %d is read after the expression but never defined in the tree",
						identifier)
				}
			}
		})
	}
}

func TestBuildReactiveFunctionTernaryValue(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   int
	}{
		{name: "single conditional", source: `function f(a, b, c) { return a ? b : c; }`, want: 1},
		{name: "nested conditional", source: `function f(a, b, c, d, e) { return a ? (b ? c : d) : e; }`, want: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			function, _ := rangesFor(t, testCase.source)
			if function == nil {
				t.Fatal("source did not lower")
			}

			graphTernaries := 0
			joinPhis := map[IdentifierId]bool{}
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
				t.Fatalf("graph holds %d Ternary terminals, want %d; the test no longer reaches "+
					"the reconstruction path", graphTernaries, testCase.want)
			}
			tree, result := BuildReactiveFunction(function)
			if tree == nil {
				t.Fatal("BuildReactiveFunction returned no tree")
			}
			if result.DoubleEmitted != 0 {
				t.Fatalf("reconstructing the conditional emitted %d block(s) twice", result.DoubleEmitted)
			}

			values, statementIfs := 0, 0
			defined := map[IdentifierId]bool{}
			VisitReactiveFunction(tree, ReactiveVisitor{
				Instruction: func(instruction *ReactiveInstruction, traverse func()) {
					if instruction.LValue != nil {
						defined[instruction.LValue.Identifier] = true
					}
					traverse()
				},
				Value: func(_ EvaluationOrder, value ReactiveValue, traverse func()) {
					if conditional, ok := value.(*ReactiveTernaryValue); ok {
						values++
						if conditional.Test == nil || conditional.Consequent == nil || conditional.Alternate == nil {
							t.Error("conditional value does not contain its test and both arms")
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
				t.Errorf("tree holds %d ReactiveTernaryValue nodes, want %d", values, testCase.want)
			}
			if statementIfs != 0 {
				t.Errorf("conditional expression became %d statement-level if(s)", statementIfs)
			}
			for identifier := range joinPhis {
				if !defined[identifier] {
					t.Errorf("conditional join phi %d is read after the expression but never defined in the tree",
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
func TestBuildReactiveFunctionLogicalKeepsRightPrefix(t *testing.T) {
	function, _ := rangesFor(t, `
		function Component(props) {
			const data = useMemo(() => {
				return props.items.edges.nodes ?? [];
			}, [props.items?.edges?.nodes]);
			return data;
		}
	`)
	if len(function.Functions) != 1 {
		t.Fatalf("component holds %d nested functions, want the useMemo callback", len(function.Functions))
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
		t.Fatalf("callback graph has %d arrays and %d logical terminals, want one of each; the "+
			"fixture no longer pins the lost-prefix shape", graphArrays, graphLogicals)
	}

	tree, result := BuildReactiveFunction(callback)
	if tree == nil {
		t.Fatal("BuildReactiveFunction returned no callback tree")
	}
	if result.DoubleEmitted != 0 {
		t.Fatalf("reconstructing the logical emitted %d block(s) twice", result.DoubleEmitted)
	}

	treeArrays, treeLogicals := 0, 0
	VisitReactiveFunction(tree, ReactiveVisitor{
		Value: func(_ EvaluationOrder, value ReactiveValue, traverse func()) {
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
		t.Errorf("tree holds %d logical values, want %d", treeLogicals, graphLogicals)
	}
	if treeArrays != graphArrays {
		t.Errorf("tree holds %d array allocations, want all %d from the graph", treeArrays, graphArrays)
	}
}

// TestBuildReactiveFunctionCorpusConservation is the same conservation property over real source.
//
// The four cases above prove the property on constructs written to exercise it; this proves it did
// not stop holding on code nobody wrote for this test. The two are not redundant: the hand-written
// cases localise a failure to one construct, and this one is the only thing that would notice a
// construct nobody thought to write a case for.
//
// # The input is the graph the PIPELINE hands this pass, and that is load-bearing
//
// `BuildReactiveScopeTerminals` runs before this stage and REWRITES the graph, so a measurement
// taken on a graph without scope terminals is not a measurement of this pass's real input. The
// difference is not cosmetic and it was measured rather than assumed: over the same 400 files the
// converter reports `doubleEmitted=0 unmatchedGotos=0` before terminals are built and
// `doubleEmitted=21 unmatchedGotos=61` after. An earlier spelling of this test omitted the terminal
// pass, passed clean on both counters, and would have pinned a green that hides every one of those
// 82 findings.
//
// The corpus numbers in this file's comments were taken at 400 files with the same walk
// `forEachCorpusFunction` performs, WITH terminals built. A number taken from a different
// denominator, or from a graph at a different pipeline stage, is not comparable to them.
func TestBuildReactiveFunctionCorpusConservation(t *testing.T) {
	converted, lostWithValueTerminal, lostWithout := 0, 0, 0
	doubleEmitted, unmatchedGotos := 0, 0
	elidedScopeBreaks, nonImplicitScopeBreaks := 0, 0

	forEachCorpusFunction(t, 400, func(function *Function, ranges *MutableRanges, scopes *ReactiveScopes) {
		// Bring the graph to the state the pipeline actually delivers before converting it.
		aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
		BuildReactiveScopeTerminals(function, scopes, MergedScopeIdentity{Aligned: aligned, Merged: merged})

		graphInstructions := 0
		hasValueTerminal := false
		for _, block := range function.Blocks {
			if block == nil {
				continue
			}
			graphInstructions += len(block.Instructions)
			switch block.Terminal.(type) {
			case *Logical, *Ternary, *Optional, *Sequence:
				hasValueTerminal = true
			}
		}

		tree, result := BuildReactiveFunction(function)
		if tree == nil {
			return
		}
		converted++

		doubleEmitted += result.DoubleEmitted
		unmatchedGotos += result.UnmatchedGotos
		elidedScopeBreaks += result.ElidedScopeBreaks
		nonImplicitScopeBreaks += result.NonImplicitScopeBreaks

		if graphInstructions > 0 && result.Instructions < graphInstructions {
			if hasValueTerminal {
				lostWithValueTerminal++
			} else {
				lostWithout++
			}
		}
	})

	if converted < 100 {
		t.Fatalf("only %d functions converted; the corpus walk is not reaching real source and "+
			"every count below would be a fact about the harness", converted)
	}

	// The initial conversion lost instructions in 353 functions with a value terminal. Rebuilding
	// logical, ternary and optional terminals through `visitValueBlockTerminal` reduces that to four:
	// the ordinary lowering shapes now become compound values, including nested expressions, while
	// a value subtree carrying a phi or an unsupported terminal deliberately takes the conservative
	// statement fallback. The remaining gap is asserted below rather than hidden in the improvement.
	//
	// A function losing instructions with no value terminal is a real bug, and it is the
	// exact shape that was already found once here: the first spelling of `valueOf` returned only a
	// block's last instruction, which cost 27 functions holding a `ForOf` whose `Test` and `Init`
	// are read through it.
	// Four functions lose instructions with no value terminal, which the declared gap does NOT
	// explain. Measured, attributed and pinned rather than tolerated: every one of the four also
	// carries an unmatched goto (both=4, lost-but-matched=0 over the same walk), so the loss is the
	// subset of the unmatched-goto population whose orphaned target held instructions. The cause is
	// the scope terminals built above -- before that pass the same walk reports zero of both.
	//
	// A ceiling, for the same reason as the two below: this is a known defect with a named cause,
	// and the test's job is to stop it growing while it waits to be fixed.
	//
	// This briefly rose to five when frozen propagation split another scope. Structured value
	// reconstruction brings it back to four while moving the aggregate control-flow counters from
	// 22 double emissions / 60 unmatched gotos to 21 / 61. One block is no longer emitted twice or
	// lost with its target; the goto reaching that target is now classified as unmatched instead.
	// The total anomaly count stays 82, so this is a tighter loss bound around the same break-target
	// defect rather than evidence that the defect closed.
	const knownLostWithoutValueTerminal = 4
	if lostWithout > knownLostWithoutValueTerminal {
		t.Errorf("%d function(s) lost instructions with NO value terminal, up from the measured "+
			"%d; the declared gap explains only value terminals, so this is unattributed loss",
			lostWithout, knownLostWithoutValueTerminal)
	}
	if lostWithout < knownLostWithoutValueTerminal {
		t.Errorf("unattributed loss is %d, down from %d; if the break-target handling was fixed, "+
			"lower this bound so the improvement is held", lostWithout, knownLostWithoutValueTerminal)
	}

	// Double emission and unmatched gotos are pinned at their measured values rather than at zero,
	// because on the graph the pipeline really delivers they are NOT zero and saying otherwise
	// would be a green that hides them. Both are caused by the scope terminals built above: the
	// same walk reports 0 and 0 on the graph before that pass runs.
	//
	// Held as a CEILING so the number can only be driven down. A drop is the fix landing and the
	// signal to lower the bound; a rise is a regression this test exists to catch. Neither is
	// allowed to happen quietly, which is the whole point of pinning a known-bad number instead of
	// deleting the assertion.
	//
	// 21 to 22 and 61 to 60 together, by the declaration-id fix in `lower.go`. One block trades for
	// one goto, which is what a recovered scope does here: a scope that survives adds a terminal to
	// break to, so a goto that previously found nothing now matches, and the block it lands in is
	// reached from one more place. The scopes recovered are named at `knownSurvivedExact` in
	// `scope_oracle_test.go`.
	//
	// Structured logical/ternary/optional reconstruction reverses that aggregate trade: 22 to 21
	// double emissions and 60 to 61 unmatched gotos. Their sum remains 82, and non-implicit scope
	// breaks remain 85, so the unresolved population did not grow; one failure moved from repeated
	// traversal to the missing-break-target arm.
	const (
		knownDoubleEmitted  = 21
		knownUnmatchedGotos = 61
	)
	// A note for whoever tightens this: `valueOf`'s own double-emit guard is NOT what these 21
	// come from. Removing that guard entirely leaves the count at exactly 21, so it never fires on
	// this corpus and a mutation of it is unmeasurable here. The 21 are counted on the statement
	// path instead. Recorded because a surviving mutant on that guard means the corpus lacks the
	// input, not that the guard is dead code.
	if doubleEmitted > knownDoubleEmitted {
		t.Errorf("%d block(s) emitted twice across the corpus, up from the measured %d; a block "+
			"reached from two places must be scheduled and broken to, not walked again",
			doubleEmitted, knownDoubleEmitted)
	}
	if unmatchedGotos > knownUnmatchedGotos {
		t.Errorf("%d goto(s) found no enclosing construct to break to, up from the measured %d",
			unmatchedGotos, knownUnmatchedGotos)
	}
	if doubleEmitted < knownDoubleEmitted || unmatchedGotos < knownUnmatchedGotos {
		t.Errorf("double emission is %d (was %d) and unmatched gotos %d (was %d); if these were "+
			"fixed, lower the bounds in this test so the improvement is held rather than "+
			"re-openable", doubleEmitted, knownDoubleEmitted, unmatchedGotos, knownUnmatchedGotos)
	}

	// Held as a floor rather than an equality. The remaining structured-value gap is still declared,
	// so a zero here means either that the gap closed or the measurement stopped reaching it; both
	// require updating the declaration and this assertion together.
	if lostWithValueTerminal == 0 {
		t.Errorf("no function lost instructions at a value terminal, but " +
			"ReactiveFunctionGapValueExpressions is still declared; either the gap closed and " +
			"should be removed, or the measurement stopped reading")
	}

	// The scope-fallthrough elision, asserted as a floor because a zero here would mean
	// `scopeFallthroughs` is never populated rather than never needed -- and that is exactly the
	// state this pass was in before the set existed, when it emitted 2,842 `break` statements the
	// source does not contain. Upstream omits every one of them.
	if elidedScopeBreaks == 0 {
		t.Errorf("no break to a scope fallthrough was elided across %d functions holding 2,874 "+
			"scope terminals; the set is not being populated, so the tree carries invented breaks",
			converted)
	}
	// Upstream asserts this is impossible and aborts; a linter counts instead. The count is the
	// ROOT of everything else this test pins, which is measured rather than argued: over the same
	// walk, 92 functions carry a non-implicit scope break, 61 carry an unmatched goto, and the
	// overlap is 61 with zero unmatched-only. The unmatched gotos are therefore a strict subset --
	// the cases severe enough that `breakTarget` found nothing at all -- and the four instruction
	// losses are a subset of those in turn.
	//
	// So this is ONE defect with three symptoms at three severities, not three defects. Driving
	// this to zero should take the other two with it.
	// # Moved when a callback's mutation of its parameter began widening the receiver's range
	//
	// 92 to 93. One function in the corpus gains a non-implicit break, and the two severer symptoms
	// this comment calls subsets of it do NOT move: `unmatchedGotos` holds at 61 and the instruction
	// losses at 4. So the population grew by one at the mildest severity and the defect did not
	// deepen, which is the distinction this test's own model of "one defect, three severities" is
	// built to express.
	//
	// Worth stating plainly because the message below says upstream raises an invariant: that is
	// about the shape, not about this corpus. The corpus here is `libraries/structure/source`, real
	// TypeScript with no upstream counterpart, so this count has no parity reference and 92 was
	// already a measured defect rather than a target.
	//
	// 86 with the two frozen-propagation edges. Falling is the improvement this bound names, and it
	// falls because a frozen value stops widening a scope across a call, so fewer scopes reach a
	// break that has to be spelled out. `unmatchedGotos` holds at 60 and `doubleEmitted` at 22.
	//
	// 85 with the frozen-capture rule in `ranges.go`. Falling is the improvement this bound names,
	// and it falls for the same reason as the move to 86: a value that stops widening leaves fewer
	// scopes reaching a break that has to be spelled out. `unmatchedGotos` holds at 60,
	// `doubleEmitted` at 22, and unattributed loss at 5.
	const knownNonImplicitScopeBreaks = 85
	if nonImplicitScopeBreaks > knownNonImplicitScopeBreaks {
		t.Errorf("%d break(s) to a scope fallthrough were not implicit, up from the measured %d; "+
			"upstream raises an invariant here, so this is a control-flow stack the walk built "+
			"differently", nonImplicitScopeBreaks, knownNonImplicitScopeBreaks)
	}
	if nonImplicitScopeBreaks < knownNonImplicitScopeBreaks {
		t.Errorf("non-implicit scope breaks are %d, down from %d; lower this bound and check "+
			"whether the unmatched-goto and instruction-loss counts fell with it",
			nonImplicitScopeBreaks, knownNonImplicitScopeBreaks)
	}

	t.Logf("converted=%d lostWithValueTerminal=%d lostWithout=%d doubleEmitted=%d unmatchedGotos=%d "+
		"elidedScopeBreaks=%d nonImplicitScopeBreaks=%d",
		converted, lostWithValueTerminal, lostWithout, doubleEmitted, unmatchedGotos,
		elidedScopeBreaks, nonImplicitScopeBreaks)
}

// TestReactiveFunctionGapsAreDeclared pins the gap list so closing one is a visible event.
//
// `ReactiveFunctionGaps` exists to be asserted on, for the reason its own comment gives: a gap that
// closes silently is an improvement nobody can point at, and a gap that opens silently is a
// regression nobody can either.
func TestReactiveFunctionGapsAreDeclared(t *testing.T) {
	gaps := ReactiveFunctionGaps()
	if len(gaps) != 4 {
		t.Fatalf("expected 4 declared gaps, found %d; a gap was added or closed without this "+
			"test and its consumers being updated", len(gaps))
	}

	seen := map[ReactiveFunctionGap]bool{}
	for _, gap := range gaps {
		if seen[gap] {
			t.Errorf("gap %v is listed twice", gap)
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
			t.Errorf("gap %v is declared as a constant but not returned by ReactiveFunctionGaps, "+
				"so nothing asserts on it", required)
		}
	}
}
