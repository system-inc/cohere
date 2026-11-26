package high_level_intermediate_representation

import (
	"testing"

	"github.com/system-inc/cohere/modules/static_single_assignment"
)

// TestPruneUnusedScopesKeepsScopesHoldingAReturn pins the condition that is not obvious.
//
// A scope containing a `return` is kept even with no declarations at all, because the value being
// memoized is early-returned from inside it and `propagateEarlyReturns` rewrites that shape later.
// Pruning it here would delete a scope a downstream pass still needs, and nothing about the
// resulting tree would look wrong.
func TestPruneUnusedScopesKeepsScopesHoldingAReturn(test *testing.T) {
	test.Parallel()

	// A scope with no declarations at all, so only the return condition can keep it.
	withReturn := &ReactiveScopeBlock{Scope: 1, Instructions: ReactiveBlock{
		&ReactiveTerminalStatement{Terminal: &ReactiveReturn{}},
	}}
	tree := &ReactiveFunction{Body: ReactiveBlock{withReturn}}
	if PruneUnusedScopes(tree, &ScopeDependencies{}) != 0 {
		test.Error("a scope holding a return was pruned; the memoized value is early-returned from " +
			"inside it and propagateEarlyReturns needs the scope standing")
	}
	if withReturn.Pruned {
		test.Error("the scope was marked pruned")
	}

	// The control: the same scope without the return must prune, or the case above passes for the
	// wrong reason.
	withoutReturn := &ReactiveScopeBlock{Scope: 1}
	control := &ReactiveFunction{Body: ReactiveBlock{withoutReturn}}
	if PruneUnusedScopes(control, &ScopeDependencies{}) != 1 {
		test.Fatal("the control scope, identical but for the return, was not pruned; the case above " +
			"is therefore not testing the return condition")
	}

	// A return nested inside a terminal still counts, which is the arm a shallow walk would miss.
	nested := &ReactiveScopeBlock{Scope: 1, Instructions: ReactiveBlock{
		&ReactiveTerminalStatement{Terminal: &ReactiveIf{
			Consequent: ReactiveBlock{
				&ReactiveTerminalStatement{Terminal: &ReactiveReturn{}},
			},
		}},
	}}
	nestedTree := &ReactiveFunction{Body: ReactiveBlock{nested}}
	if PruneUnusedScopes(nestedTree, &ScopeDependencies{}) != 0 {
		test.Error("a scope whose return sits inside an if was pruned; the walk is not descending " +
			"into terminals")
	}
}

// TestPruneUnusedScopesKeepsScopesWithOwnDeclarations covers `hasOwnDeclaration`.
//
// The distinction that matters is between a declaration a scope produced and one that bubbled up
// from a scope nested inside it. Only the first keeps the scope, and telling them apart is the
// entire reason `ScopeDependencies` records an origin.
func TestPruneUnusedScopesKeepsScopesWithOwnDeclarations(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name        string
		origin      ScopeId
		originKnown bool
		wantPruned  bool
	}{
		{name: "declares its own value", origin: 1, originKnown: true, wantPruned: false},
		{name: "declaration bubbled up from within", origin: 2, originKnown: true, wantPruned: true},
		{name: "origin unknown keeps the scope", originKnown: false, wantPruned: false},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			dependencies := &ScopeDependencies{
				declarations: map[ScopeId][]static_single_assignment.IdentifierId{1: {7}},
			}
			if testCase.originKnown {
				dependencies.declarationOrigin = map[static_single_assignment.IdentifierId]ScopeId{7: testCase.origin}
			}

			scope := &ReactiveScopeBlock{Scope: 1}
			tree := &ReactiveFunction{Body: ReactiveBlock{scope}}
			PruneUnusedScopes(tree, dependencies)

			if scope.Pruned != testCase.wantPruned {
				test.Errorf("pruned=%t, want %t", scope.Pruned, testCase.wantPruned)
			}
		})
	}
}

// TestPruneUnusedScopesKeepsScopesWithReassignments covers the arm the corpus barely exercises.
//
// Only 2 of 1,576 corpus scopes carry a reassignment, so corpus coverage says almost nothing here.
func TestPruneUnusedScopesKeepsScopesWithReassignments(test *testing.T) {
	test.Parallel()

	dependencies := &ScopeDependencies{
		reassignments: map[ScopeId][]static_single_assignment.IdentifierId{1: {7}},
	}
	scope := &ReactiveScopeBlock{Scope: 1}
	tree := &ReactiveFunction{Body: ReactiveBlock{scope}}

	if PruneUnusedScopes(tree, dependencies) != 0 {
		test.Error("a scope carrying a reassignment was pruned")
	}

	// The control, so this is known to be the reassignment doing the work.
	bare := &ReactiveScopeBlock{Scope: 1}
	control := &ReactiveFunction{Body: ReactiveBlock{bare}}
	if PruneUnusedScopes(control, &ScopeDependencies{}) != 1 {
		test.Fatal("the control scope was not pruned, so the case above is not testing reassignments")
	}
}
