package high_level_intermediate_representation

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/typescript_language/rule"
	"github.com/system-inc/cohere/internal/typescript_language/rule_testing"
)

// TestAlignMethodCallScopesRemovesTheInBetweenState asserts the pass's whole contract.
//
// After it runs, a `MethodCall` has either both sides scoped together or neither side scoped. The
// before assertion keeps that result from passing vacuously, and the call count distinguishes a
// lowering failure from a fixture that simply no longer reaches the in-between state.
func TestAlignMethodCallScopesRemovesTheInBetweenState(test *testing.T) {
	test.Parallel()

	// A known primitive-returning method is the live shape after call-result allocation became
	// faithful. Its result does not allocate, while its PropertyLoad result is scoped, so alignment
	// must remove the property's scope.
	//
	// Unknown and known-mutable calls are no longer suitable controls here: upstream's mayAllocate
	// rule gives their results a scope, and the disjoint pass joins that result with the property
	// before this pass runs. The population measurement below records which other arms remain absent.
	const source = `
		function contains(items: string[], selected: string) {
			return items.includes(selected);
		}
	`
	before, after, calls := methodCallScopeStates(test, source)
	if calls == 0 {
		test.Fatal("no method call was reached, so this test asserts nothing about the pass; the " +
			"fixture stopped exercising it rather than the pass being correct")
	}
	if before == 0 {
		test.Fatal("no method call was in the in-between state before the pass, so a clean result " +
			"after it proves nothing")
	}
	if after != 0 {
		test.Errorf("%d method call(s) still have one side scoped and the other not, down from %d; "+
			"the pass exists to make that count zero", after, before)
	}
	test.Logf("method calls: %d reached, %d in the in-between state before, %d after", calls, before, after)
}

// methodCallScopeStates counts method calls whose two sides disagree about scopes.
func methodCallScopeStates(test *testing.T, source string) (before, after, calls int) {
	test.Helper()
	probe := rule.Rule[rule.NoOptions]{
		Name:             "method-call-scope-states",
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
						InferReactive(function, ruleContext.TypeChecker)
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
	rule_testing.RunTypedFiles(test, probe, map[string]string{
		"/react.d.ts":  reactiveDeclarations,
		"/fixture.tsx": source,
	}, "/fixture.tsx")
	return before, after, calls
}
