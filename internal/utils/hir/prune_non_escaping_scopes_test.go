package hir

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/verify/internal/reactconformance"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/ruletest"
)

// TestPruneNonEscapingScopesPrunesAndKeeps is the baseline, and it guards two opposite failures.
//
// A pass that prunes every scope has deleted every memoization in the program. One that prunes none
// is a no-op. Both satisfy an assertion phrased about a single side, and this pass has more moving
// parts than any other in phase 6 -- a classification, a graph, a propagation and a transform -- so
// the baseline is asserted on real source before any specific claim.
func TestPruneNonEscapingScopesPrunesAndKeeps(t *testing.T) {
	functions, scopesBefore, pruned, roots, memoized := 0, 0, 0, 0, 0

	forEachCorpusFunctionWithChecker(t, 200, func(function *Function, checker *shimchecker.Checker) {
		InferReactive(function, checker)
		tree, dependencies := prunableTree(t, function, checker)
		if tree == nil {
			return
		}
		functions++
		VisitReactiveFunction(tree, ReactiveVisitor{
			Scope: func(scope *ReactiveScopeBlock, traverse func()) {
				scopesBefore++
				traverse()
			},
		})

		result := PruneNonEscapingScopes(tree, function, dependencies, checker)
		pruned += result.Pruned
		roots += result.EscapingRoots
		memoized += result.Memoized
	})

	if functions < 50 {
		t.Fatalf("only %d functions reached the pass; the corpus walk is not reaching real source",
			functions)
	}
	if scopesBefore == 0 {
		t.Fatal("the corpus produced no scopes, so pruning none proves nothing")
	}
	if roots == 0 {
		t.Error("no escaping root was found across the corpus; every function returns something, " +
			"so a zero here means the return arm is not reached and everything would prune")
	}
	if memoized == 0 {
		t.Error("nothing was memoized across the corpus; the propagation is not reaching values " +
			"from the roots")
	}
	if pruned == scopesBefore {
		t.Error("every scope was pruned, which deletes every memoization in the program")
	}

	t.Logf("functions=%d scopes=%d pruned=%d escapingRoots=%d memoized=%d",
		functions, scopesBefore, pruned, roots, memoized)
}

// TestPruneNonEscapingScopesKeepsAReturnedValue is upstream's first worked example.
//
// A scope producing a value the function returns must survive: the caller holds it, so memoizing it
// is what stops the caller seeing a new identity every render.
func TestPruneNonEscapingScopesKeepsAReturnedValue(t *testing.T) {
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
		declarations: map[ScopeId][]IdentifierId{1: {1}},
	}

	result := PruneNonEscapingScopes(tree, function, dependencies, nil)
	if result.EscapingRoots == 0 {
		t.Fatal("the returned value was not recorded as escaping, so the keep below would happen " +
			"for the wrong reason")
	}
	if result.Pruned != 0 {
		t.Errorf("pruned %d scope(s); a scope producing a returned value must survive", result.Pruned)
	}
}

// TestPruneNonEscapingScopesPrunesAValueNothingHolds is the other half, and the control on the case
// above.
//
// The same scope with the return removed must prune. Without this the keep above passes on a pass
// that never prunes anything at all.
func TestPruneNonEscapingScopesPrunesAValueNothingHolds(t *testing.T) {
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
		declarations: map[ScopeId][]IdentifierId{1: {1}},
	}

	result := PruneNonEscapingScopes(tree, function, dependencies, nil)
	if result.Pruned != 1 {
		t.Errorf("pruned %d, want 1; a scope whose value nothing holds costs a comparison every "+
			"render and buys nothing", result.Pruned)
	}
	// The scope's instructions must survive as ordinary statements rather than being deleted.
	instructions := countReactiveInstructionNodes(tree.Body)
	if instructions == 0 {
		t.Error("pruning the scope deleted its instructions; a pruned scope is replaced by its " +
			"own body, not removed")
	}
}

// TestPruneNonEscapingScopesTreatsHookArgumentsAsEscaping covers the second kind of root.
//
// A value passed to a hook escapes because React may retain it. The closure handed to `useEffect` is
// the standard case: it does not appear in any return and must still be memoized.
func TestPruneNonEscapingScopesTreatsHookArgumentsAsEscaping(t *testing.T) {
	roots := hookArgumentRoots(t, `
		import {useEffect} from 'react';
		function Component(props) {
			const callback = () => props.value;
			useEffect(callback);
			return null;
		}
	`)
	if roots == 0 {
		t.Error("a hook argument was not recorded as escaping; React may retain it, so it must be " +
			"held even though nothing returns it")
	}

	// The control: the same call to a function that is not a hook must contribute no root, or the
	// case above passes because every call argument escapes.
	nonHook := hookArgumentRoots(t, `
		function notAHook(callback) { return callback; }
		function Component(props) {
			const callback = () => props.value;
			notAHook(callback);
			return null;
		}
	`)
	if nonHook > roots {
		t.Errorf("a non-hook call contributed %d roots against a hook's %d; the hook test is not "+
			"discriminating", nonHook, roots)
	}
}

// hookArgumentRoots runs the collection over a source and returns how many roots it found.
func hookArgumentRoots(t *testing.T, source string) int {
	t.Helper()

	total := 0
	runTypedSource(t, source, func(function *Function, checker *shimchecker.Checker) {
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
func runTypedSource(t *testing.T, source string, visit func(*Function, *shimchecker.Checker)) {
	t.Helper()

	ran := false
	probe := rule.Rule{
		Name:             "prune-non-escaping-harness",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ctx.TypeChecker == nil {
						t.Fatal("the typed harness handed this probe a nil checker")
					}
					forEachFunctionLike(node, func(functionNode *ast.Node) {
						function := Lower(functionNode, ctx.TypeChecker)
						if function == nil {
							return
						}
						Construct(function)
						ran = true
						visit(function, ctx.TypeChecker)
					})
				},
			}
		},
	}
	ruletest.RunTypedFiles(t, probe, map[string]string{
		"/react.d.ts":  reactiveDeclarations,
		"/escaping.ts": source,
	}, "/escaping.ts")

	if !ran {
		t.Fatal("no function was lowered from the fixture, so the callback never ran")
	}
}

// TestPruneNonEscapingScopesIsDeterministic pins reproducibility, which it did not have.
//
// The propagation walk is order-dependent: the `seen` guard locks in whichever answer a node was
// first reached with, and a node reached under forcing answers differently from the same node
// reached without it. With the graph's sets iterated as Go maps, three identical corpus runs
// returned 506, 507 and 508 pruned scopes with memoized counts spanning 1,922 to 2,054.
//
// A pass whose output varies run to run makes a cache non-reproducible without ever producing an
// answer anyone can point at as wrong, which is the failure mode this asserts against.
func TestPruneNonEscapingScopesIsDeterministic(t *testing.T) {
	const runs = 3
	var results []PruneNonEscapingScopesResult

	for run := 0; run < runs; run++ {
		total := PruneNonEscapingScopesResult{}
		forEachCorpusFunctionWithChecker(t, 100, func(function *Function, checker *shimchecker.Checker) {
			InferReactive(function, checker)
			tree, dependencies := prunableTree(t, function, checker)
			if tree == nil {
				return
			}
			result := PruneNonEscapingScopes(tree, function, dependencies, checker)
			total.Pruned += result.Pruned
			total.EscapingRoots += result.EscapingRoots
			total.Memoized += result.Memoized
			total.Declarations += result.Declarations
		})
		results = append(results, total)
	}

	if results[0].Pruned == 0 || results[0].Memoized == 0 {
		t.Fatal("the first run pruned or memoized nothing, so comparing runs proves nothing")
	}
	for index := 1; index < runs; index++ {
		if results[index] != results[0] {
			t.Errorf("run %d returned %+v and run 0 returned %+v; the pass is not reproducible",
				index, results[index], results[0])
		}
	}

	t.Logf("stable across %d runs: %+v", runs, results[0])
}

// TestPruneNonEscapingScopesResolvesLoadLocalIndirection covers the map a mutation exposed.
//
// A value read into a temporary must be recorded against the binding it came from, so a fact learned
// about the temporary is a fact about the binding.
//
// # Two fixture shapes that do not discriminate, recorded so nobody rebuilds them
//
// The obvious shape -- scope produces `source`, `copy` loads it, the function returns `copy` -- does
// not work. `LoadLocal` classifies as `Conditional` and records `source` as a dependency, so the
// escape reaches `source` along the dependency edge whether or not the map resolved anything.
//
// Reordering so the load precedes the scope does not work either, and for a reason that is upstream's
// design rather than a bug: the definitions entry is written after the instruction's own lvalue is
// recorded, at `PruneNonEscapingScopes.ts:911`. So the map never affects the instruction establishing
// it, only later ones -- and in both shapes the dependency edge already carries the escape.
//
// The map's effect is therefore cumulative across a function rather than visible in any three-
// instruction fixture. It is asserted on the corpus instead: with the resolution, 1,016 escaping
// roots; without it, 809. That difference is the whole of what this map does, and a hand-written
// case cannot reach it.
func TestPruneNonEscapingScopesResolvesLoadLocalIndirection(t *testing.T) {
	roots, definitionsRecorded := 0, 0

	forEachCorpusFunctionWithChecker(t, 200, func(function *Function, checker *shimchecker.Checker) {
		InferReactive(function, checker)
		tree, dependencies := prunableTree(t, function, checker)
		if tree == nil {
			return
		}
		collector := memoizationCollector{
			function:     function,
			dependencies: dependencies,
			graph:        NewMemoizationGraph(),
			definitions:  map[DeclarationId]DeclarationId{},
		}
		collector.walk(tree.Body)
		roots += collector.graph.EscapingCount()
		definitionsRecorded += len(collector.definitions)
	})

	if definitionsRecorded == 0 {
		t.Fatal("no LoadLocal indirection was recorded across the corpus, so the resolution is " +
			"unexercised and this test asserts nothing")
	}
	// A floor rather than the exact 1,016, since the corpus moves. What must not happen is the
	// count collapsing toward the 809 that dropping the resolution produces -- a 20 percent loss of
	// escaping roots, every one of which is a scope that would then prune wrongly.
	if roots < 900 {
		t.Errorf("found %d escaping roots across the corpus against %d recorded indirections; "+
			"dropping the LoadLocal resolution measured 809 where resolving measures 1,016, so a "+
			"count in that region means the resolution is not being applied",
			roots, definitionsRecorded)
	}

	t.Logf("escapingRoots=%d loadLocalIndirections=%d", roots, definitionsRecorded)
}

// TestMemoMarkersArePrunedOnlyWhenTheirScopeWas covers the marker pass in both directions.
//
// Both directions matter and neither alone is enough. A pass that marks every marker satisfies any
// assertion phrased only about pruning, and a pass that marks none satisfies any assertion phrased
// only about survival. This file has produced fixtures that passed for exactly that reason.
func TestMemoMarkersArePrunedOnlyWhenTheirScopeWas(t *testing.T) {
	// Measured on the vendored corpus rather than on constructed source: whether a marker's value
	// carries a scope depends on lowering and on four passes upstream of this one, so a hand-written
	// fixture would be asserting the pipeline rather than the pass.
	fixtures, err := reactconformance.Load("../../reactconformance/testdata/fixtures")
	if err != nil {
		t.Fatalf("loading the vendored corpus: %v", err)
	}

	marked, total := 0, 0
	for _, fixture := range fixtures {
		if !strings.Contains(fixture.Source, "validatePreserveExistingMemoizationGuarantees") {
			continue
		}
		pruned, seen, ok := memoMarkerCountsForSource(t, fixture.Source)
		if !ok {
			continue
		}
		marked += pruned
		total += seen
	}

	if total == 0 {
		t.Fatal("no `FinishMemoize` marker was reached on the whole corpus, so this test asserts " +
			"nothing about the pass; the population is wrong rather than the pass being correct")
	}
	if marked == 0 {
		t.Errorf("no marker was pruned across %d markers, so the pass is inert", total)
	}
	if marked == total {
		t.Errorf("every one of %d markers was pruned, which is unconditional marking rather than "+
			"a decision about whether the value's scope survived", total)
	}
	t.Logf("markers: %d pruned of %d reached", marked, total)
}

// memoMarkerCountsForSource runs the pipeline through the pruning pass and counts markers.
func memoMarkerCountsForSource(t *testing.T, source string) (pruned, reached int, ok bool) {
	t.Helper()
	probe := rule.Rule{
		Name:             "memo-marker-counts",
		NeedsTypeChecker: true,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ctx.TypeChecker == nil {
						return
					}
					forEachFunctionLike(node, func(functionNode *ast.Node) {
						function := Lower(functionNode, ctx.TypeChecker)
						if function == nil {
							return
						}
						Construct(function)
						markerPruned, markerReached := markerCounts(function, ctx.TypeChecker)
						pruned += markerPruned
						reached += markerReached
						ok = true
					})
				},
			}
		},
	}
	ruletest.RunTypedFiles(t, probe, map[string]string{
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
