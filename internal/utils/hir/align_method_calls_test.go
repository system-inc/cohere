package hir

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/ruletest"
)

// TestAlignMethodCallScopesRemovesTheInBetweenState asserts the pass's whole contract.
//
// After it runs, a `MethodCall` has either both sides scoped together or neither side scoped. Both
// directions are checked, because a pass that scoped everything and one that scoped nothing each
// satisfy an assertion phrased about one of them.
func TestAlignMethodCallScopesRemovesTheInBetweenState(t *testing.T) {
	// Two shapes, though only one arm turns out to be reachable.
	//
	// A first version carried only `console.log(...)` and a mutation deleting the call-scoped arm
	// survived it. The second shape was added to reach that arm and the mutant survived anyway,
	// which is the signal that the POPULATION is the reason rather than the fixture. Measured over
	// the corpus: the property-scoped arm fires 839 times and the other two fire zero times, because
	// a method call's lvalue is a temporary that only receives a scope if something mutates it.
	//
	// Both shapes are kept, because the second one documents what was tried, and the note at the
	// switch in the pass records the verdict and when it expires.
	const source = `
		function draw(styles: Map<number, {color: string}>, features: string[]) {
			for(const [index, style] of styles) {
				const feature = features[index];
				if(!feature) continue;
				console.log(style.color, feature);
			}
		}
		function build(items: string[]) {
			const mapped = items.map((one) => ({value: one}));
			return mapped;
		}
	`
	before, after, calls := methodCallScopeStates(t, source)
	if calls == 0 {
		t.Fatal("no method call was reached, so this test asserts nothing about the pass; the " +
			"fixture stopped exercising it rather than the pass being correct")
	}
	if before == 0 {
		t.Fatal("no method call was in the in-between state before the pass, so a clean result " +
			"after it proves nothing")
	}
	if after != 0 {
		t.Errorf("%d method call(s) still have one side scoped and the other not, down from %d; "+
			"the pass exists to make that count zero", after, before)
	}
	t.Logf("method calls: %d reached, %d in the in-between state before, %d after", calls, before, after)
}

// TestAlignMethodCallScopesReachesTheCorpus is the population check.
//
// A pass that fires on nothing passes every assertion above by never being exercised. This is the
// control for that, and the count is asserted rather than logged because a drop to zero would mean
// the pass stopped being reachable rather than stopped being needed.
func TestAlignMethodCallScopesReachesTheCorpus(t *testing.T) {
	functions, changed := 0, 0
	forEachCorpusFunctionWithChecker(t, 400, func(function *Function, checker *shimchecker.Checker) {
		InferReactive(function, checker)
		DropManualMemoization(function)
		ranges := InferMutableRanges(function)
		disjoint := FindDisjointMutableValuesWithRanges(function, ranges)
		scopes := AssignReactiveScopesWithSets(function, ranges, disjoint)
		functions++
		if AlignMethodCallScopes(function, scopes) != scopes {
			changed++
		}
	})
	if functions < 100 {
		t.Fatalf("only %d functions reached; the corpus walk is not finding real source and the "+
			"count below would be a fact about the harness", functions)
	}
	// Measured. A move here is the corpus changing or the pass changing what it touches; both are
	// worth a look rather than an adjustment.
	//
	// 363 with the two frozen-propagation edges in `effects.go` and `ranges.go`. A frozen value no
	// longer widens its range across a conditional mutation, so more method calls reach this pass
	// with their two sides in different scopes and more get aligned. The pass changing what it
	// touches is the second cause this comment names, and it is this one.
	const knownChanged = 363
	if changed != knownChanged {
		t.Errorf("the pass changed %d of %d functions, want %d", changed, functions, knownChanged)
	}
	t.Logf("the pass rewrote scopes in %d of %d corpus functions", changed, functions)
}

// methodCallScopeStates counts method calls whose two sides disagree about scopes.
func methodCallScopeStates(t *testing.T, source string) (before, after, calls int) {
	t.Helper()
	probe := rule.Rule{
		Name:             "method-call-scope-states",
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
						InferReactive(function, ctx.TypeChecker)
						DropManualMemoization(function)
						ranges := InferMutableRanges(function)
						disjoint := FindDisjointMutableValuesWithRanges(function, ranges)
						scopes := AssignReactiveScopesWithSets(function, ranges, disjoint)
						aligned := AlignMethodCallScopes(function, scopes)

						disagreements := func(table *ReactiveScopes) int {
							count := 0
							for _, instruction := range function.Instructions {
								if instruction == nil {
									continue
								}
								call, isMethodCall := instruction.Value.(*MethodCall)
								if !isMethodCall {
									continue
								}
								lvalue := table.ScopeOf(instruction.LValue.Identifier)
								property := table.ScopeOf(call.Property.Identifier)
								if lvalue != property {
									count++
								}
							}
							return count
						}
						for _, instruction := range function.Instructions {
							if instruction == nil {
								continue
							}
							if _, isMethodCall := instruction.Value.(*MethodCall); isMethodCall {
								calls++
							}
						}
						before += disagreements(scopes)
						after += disagreements(aligned)
					})
				},
			}
		},
	}
	ruletest.RunTypedFiles(t, probe, map[string]string{
		"/react.d.ts":  reactiveDeclarations,
		"/fixture.tsx": source,
	}, "/fixture.tsx")
	return before, after, calls
}
