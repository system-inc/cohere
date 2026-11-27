package high_level_intermediate_representation

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/rules/react/conformance"
	"github.com/system-inc/cohere/internal/lint/testing"
	"github.com/system-inc/cohere/modules/static_single_assignment"
)

// TestPruneNonEscapingScopesKeepsAReturnedValue is upstream's first worked example.
//
// A scope producing a value the function returns must survive: the caller holds it, so memoizing it
// is what stops the caller seeing a new identity every render.
func TestPruneNonEscapingScopesKeepsAReturnedValue(test *testing.T) {
	test.Parallel()

	function := &Function{Identifiers: []*Identifier{{Id: 0}, {Id: 1}}}
	produced := Place{Identifier: 1}

	scope := &ReactiveScopeBlock{Scope: 1, Instructions: ReactiveBlock{
		&ReactiveInstructionStatement{Instruction: &ReactiveInstruction{
			LValue: &produced,
			Value:  &ReactiveInstructionValue{Value: &ArrayExpression{}},
		}},
	}}
	tree := &ReactiveFunction{Body: ReactiveBlock{
		scope,
		&ReactiveTerminalStatement{Terminal: &ReactiveReturn{Value: produced}},
	}}
	dependencies := &ScopeDependencies{
		declarations: map[ScopeId][]static_single_assignment.IdentifierId{1: {1}},
	}

	result := PruneNonEscapingScopes(tree, function, dependencies, nil)
	if result.EscapingRoots == 0 {
		test.Fatal("the returned value was not recorded as escaping, so the keep below would happen " +
			"for the wrong reason")
	}
	if result.Pruned != 0 {
		test.Errorf("pruned %d scope(s); a scope producing a returned value must survive", result.Pruned)
	}
}

// TestPruneNonEscapingScopesPrunesAValueNothingHolds is the other half, and the control on the case
// above.
//
// The same scope with the return removed must prune. Without this the keep above passes on a pass
// that never prunes anything at all.
func TestPruneNonEscapingScopesPrunesAValueNothingHolds(test *testing.T) {
	test.Parallel()

	function := &Function{Identifiers: []*Identifier{{Id: 0}, {Id: 1}}}
	produced := Place{Identifier: 1}

	scope := &ReactiveScopeBlock{Scope: 1, Instructions: ReactiveBlock{
		&ReactiveInstructionStatement{Instruction: &ReactiveInstruction{
			LValue: &produced,
			Value:  &ReactiveInstructionValue{Value: &ArrayExpression{}},
		}},
	}}
	tree := &ReactiveFunction{Body: ReactiveBlock{scope}}
	dependencies := &ScopeDependencies{
		declarations: map[ScopeId][]static_single_assignment.IdentifierId{1: {1}},
	}

	result := PruneNonEscapingScopes(tree, function, dependencies, nil)
	if result.Pruned != 1 {
		test.Errorf("pruned %d, want 1; a scope whose value nothing holds costs a comparison every "+
			"render and buys nothing", result.Pruned)
	}
	// The scope's instructions must survive as ordinary statements rather than being deleted.
	instructions := countReactiveInstructionNodes(tree.Body)
	if instructions == 0 {
		test.Error("pruning the scope deleted its instructions; a pruned scope is replaced by its " +
			"own body, not removed")
	}
}

// TestPruneNonEscapingScopesTreatsHookArgumentsAsEscaping covers the second kind of root.
//
// A value passed to a hook escapes because React may retain it. The closure handed to `useEffect` is
// the standard case: it does not appear in any return and must still be memoized.
func TestPruneNonEscapingScopesTreatsHookArgumentsAsEscaping(test *testing.T) {
	test.Parallel()

	roots := hookArgumentRoots(test, `
		import {useEffect} from 'react';
		function Component(props) {
			const callback = () => props.value;
			useEffect(callback);
			return null;
		}
	`)
	if roots == 0 {
		test.Error("a hook argument was not recorded as escaping; React may retain it, so it must be " +
			"held even though nothing returns it")
	}

	// The control: the same call to a function that is not a hook must contribute no root, or the
	// case above passes because every call argument escapes.
	nonHook := hookArgumentRoots(test, `
		function notAHook(callback) { return callback; }
		function Component(props) {
			const callback = () => props.value;
			notAHook(callback);
			return null;
		}
	`)
	if nonHook > roots {
		test.Errorf("a non-hook call contributed %d roots against a hook's %d; the hook test is not "+
			"discriminating", nonHook, roots)
	}
}

// hookArgumentRoots runs the collection over a source and returns how many roots it found.
func hookArgumentRoots(test *testing.T, source string) int {
	test.Helper()

	total := 0
	runTypedSource(test, source, func(function *Function, checker *shimchecker.Checker) {
		tree, _ := BuildReactiveFunction(function)
		if tree == nil {
			return
		}
		total += PruneNonEscapingScopes(tree, function, &ScopeDependencies{}, checker).EscapingRoots
	})
	return total
}

// runTypedSource lowers every function in a source and hands each to visit with the checker.
//
// The React declarations are needed because `IsHookCallee` resolves a callee through its syntactic
// node, and an unresolved import gives no node to read a name from.
func runTypedSource(test *testing.T, source string, visit func(*Function, *shimchecker.Checker)) {
	test.Helper()

	ran := false
	probe := rule.Rule[rule.NoOptions]{
		Name:             "prune-non-escaping-harness",
		NeedsTypeChecker: true,
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ruleContext.TypeChecker == nil {
						test.Fatal("the typed harness handed this probe a nil checker")
					}
					forEachFunctionLike(node, func(functionNode *ast.Node) {
						function := Lower(functionNode, ruleContext.TypeChecker)
						if function == nil {
							return
						}
						Construct(function)
						ran = true
						visit(function, ruleContext.TypeChecker)
					})
				},
			}
		},
	}
	rule_testing.RunTypedFiles(test, probe, map[string]string{
		"/react.d.ts":  reactiveDeclarations,
		"/escaping.ts": source,
	}, "/escaping.ts")

	if !ran {
		test.Fatal("no function was lowered from the fixture, so the callback never ran")
	}
}

// A sequence remains a list of real instructions when it is nested inside another reactive value.
// Optional and nullish-coalescing callbacks produce exactly this shape after IIFE inlining: the
// allocation and the load that carries it to the join live in a sequence under a logical value.
// Missing that load leaves the join declaration at MemoizationNever, so an escaping useMemo result
// is treated as non-escaping and its scope is pruned.
func TestPruneNonEscapingScopesVisitsASequenceNestedInALogical(test *testing.T) {
	test.Parallel()

	function := &Function{Identifiers: []*Identifier{
		{Id: 0, Declaration: 1}, // allocation
		{Id: 1, Declaration: 2}, // value carried out of the sequence
		{Id: 2, Declaration: 3}, // logical result
	}}
	allocation := Place{Identifier: 0}
	carried := Place{Identifier: 1}
	result := Place{Identifier: 2}

	nested := &ReactiveSequenceValue{
		Instructions: []*ReactiveInstruction{
			{LValue: &allocation, Value: &ReactiveInstructionValue{Value: &ArrayExpression{}}},
			{LValue: &carried, Value: &ReactiveInstructionValue{Value: &LoadLocal{Place: allocation}}},
		},
		Value: &ReactiveInstructionValue{Value: &LoadLocal{Place: carried}},
	}
	logical := &ReactiveInstruction{
		LValue: &result,
		Value: &ReactiveLogicalValue{
			Operator: "??",
			Left:     &ReactiveInstructionValue{Value: &Primitive{Value: PrimitiveUndefined{}}},
			Right:    nested,
		},
	}
	collector := memoizationCollector{
		function:    function,
		graph:       NewMemoizationGraph(),
		definitions: map[static_single_assignment.DeclarationId]static_single_assignment.DeclarationId{},
	}
	collector.visitInstruction(logical)

	if got := collector.definitions[2]; got != 1 {
		test.Fatalf("nested LoadLocal resolved declaration 2 to %d, want allocation declaration 1; "+
			"the sequence instructions were not visited through the logical value", got)
	}
	collector.graph.MarkEscaping(collector.resolve(2))
	if memoized := collector.graph.ComputeMemoized(); !memoized[1] {
		test.Errorf("allocation declaration 1 is not memoized after its carried value escapes: %v",
			memoized)
	}
}

// TestMemoMarkersArePrunedOnlyWhenTheirScopeWas covers the marker pass in both directions.
//
// Both directions matter and neither alone is enough. A pass that marks every marker satisfies any
// assertion phrased only about pruning, and a pass that marks none satisfies any assertion phrased
// only about survival. This file has produced fixtures that passed for exactly that reason.
func TestMemoMarkersArePrunedOnlyWhenTheirScopeWas(test *testing.T) {
	test.Parallel()

	// Measured on the vendored corpus rather than on constructed source: whether a marker's value
	// carries a scope depends on lowering and on four passes upstream of this one, so a hand-written
	// fixture would be asserting the pipeline rather than the pass.
	fixtures, loadError := react_conformance.Load("../../rules/react/conformance/testdata/fixtures")
	if loadError != nil {
		test.Fatalf("loading the vendored corpus: %v", loadError)
	}

	marked, total := 0, 0
	for _, fixture := range fixtures {
		if !strings.Contains(fixture.Source, "validatePreserveExistingMemoizationGuarantees") {
			continue
		}
		pruned, seen, ok := memoMarkerCountsForSource(test, fixture.Source)
		if !ok {
			continue
		}
		marked += pruned
		total += seen
	}

	if total == 0 {
		test.Fatal("no `FinishMemoize` marker was reached on the whole corpus, so this test asserts " +
			"nothing about the pass; the population is wrong rather than the pass being correct")
	}
	if marked == 0 {
		test.Errorf("no marker was pruned across %d markers, so the pass is inert", total)
	}
	if marked == total {
		test.Errorf("every one of %d markers was pruned, which is unconditional marking rather than "+
			"a decision about whether the value's scope survived", total)
	}
	test.Logf("markers: %d pruned of %d reached", marked, total)
}

// memoMarkerCountsForSource runs the pipeline through the pruning pass and counts markers.
func memoMarkerCountsForSource(test *testing.T, source string) (pruned, reached int, ok bool) {
	test.Helper()
	probe := rule.Rule[rule.NoOptions]{
		Name:             "memo-marker-counts",
		NeedsTypeChecker: true,
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ruleContext.TypeChecker == nil {
						return
					}
					forEachFunctionLike(node, func(functionNode *ast.Node) {
						function := Lower(functionNode, ruleContext.TypeChecker)
						if function == nil {
							return
						}
						Construct(function)
						markerPruned, markerReached := markerCounts(function, ruleContext.TypeChecker)
						pruned += markerPruned
						reached += markerReached
						ok = true
					})
				},
			}
		},
	}
	rule_testing.RunTypedFiles(test, probe, map[string]string{
		"/react.d.ts":  reactiveDeclarations,
		"/fixture.tsx": source,
	}, "/fixture.tsx")
	return pruned, reached, ok
}

func markerCounts(function *Function, checker *shimchecker.Checker) (pruned, reached int) {
	InferReactive(function, checker)
	DropManualMemoization(function)
	ranges := InferMutableRanges(function)
	disjoint := FindDisjointMutableValuesWithRanges(function, ranges)
	scopes := AssignReactiveScopesWithSets(function, ranges, disjoint)
	aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
	identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
	BuildReactiveScopeTerminals(function, scopes, identity)
	dependencies := CollectScopeDependenciesWithHoistable(function, scopes, identity, ranges)
	tree, _ := BuildReactiveFunction(function)
	if tree == nil {
		return 0, 0
	}
	MergeReactiveScopesThatInvalidateTogether(tree, function, dependencies, checker)
	result := PruneNonEscapingScopesWithScopes(tree, function, dependencies, scopes, checker)

	TransformReactiveFunction(tree, ReactiveTransformer{
		Instruction: func(statement *ReactiveInstructionStatement,
			traverse func()) ReactiveTransformed {
			traverse()
			if statement == nil || statement.Instruction == nil {
				return KeepStatement()
			}
			plain, isPlain := statement.Instruction.Value.(*ReactiveInstructionValue)
			if !isPlain || plain == nil {
				return KeepStatement()
			}
			if _, isFinish := plain.Value.(*FinishMemoize); isFinish {
				reached++
			}
			return KeepStatement()
		},
	})
	return result.MarkersPruned, reached
}
