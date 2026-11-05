package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

type nonReactivePruner struct {
	function     *Function
	dependencies *ScopeDependencies
	reactive     map[static_single_assignment.IdentifierId]bool
	// seeded is how many identifiers the seed contributed, before propagation.
	seeded int
	pruned int
}

// walk visits a block, pruning each scope's dependencies innermost-first.
//
// Innermost-first because the propagation runs outward: an inner scope's outputs become reactive
// before an enclosing scope asks whether its own dependencies are.
func (pruner *nonReactivePruner) walk(block ReactiveBlock) {
	for _, statement := range block {
		switch shape := statement.(type) {
		case *ReactiveScopeBlock:
			pruner.walk(shape.Instructions)
			// A pruned scope is walked and nothing more. Upstream's visitor overrides `visitScope`
			// only, and the base `visitPrunedScope` visits the body, so a pruned scope neither drops
			// its dependencies nor makes its declarations reactive. Propagating through one marked
			// a hook call's declarations reactive, `useRef`'s result among them, and a memo callback
			// nested beside it then kept the ref as a dependency the developer never wrote.
			if shape.Pruned {
				continue
			}
			pruner.visitScope(shape)
		case *ReactiveTerminalStatement:
			pruner.walkTerminal(shape)
		}
	}
}

// visitScope drops a scope's non-reactive dependencies, then propagates outward if any survived.
func (pruner *nonReactivePruner) visitScope(scope *ReactiveScopeBlock) {
	removed := pruner.dependencies.PruneNonReactiveDependenciesOf(scope.Scope, pruner.reactive)
	pruner.pruned += removed

	if len(pruner.dependencies.DependenciesOf(scope.Scope)) == 0 {
		return
	}
	// Upstream's comment: if any dependency is reactive then every output re-evaluates whenever
	// that dependency changes, so the outputs are reactive in practice even when the values
	// themselves would not be.
	for _, declared := range pruner.dependencies.DeclarationsOf(scope.Scope) {
		pruner.reactive[declared] = true
	}
	for _, reassigned := range pruner.dependencies.ReassignmentsOf(scope.Scope) {
		pruner.reactive[reassigned] = true
	}
}

// walkTerminal descends into every block a terminal contains.
func (pruner *nonReactivePruner) walkTerminal(statement *ReactiveTerminalStatement) {
	switch shape := statement.Terminal.(type) {
	case *ReactiveIf:
		pruner.walk(shape.Consequent)
		if shape.Alternate != nil {
			pruner.walk(*shape.Alternate)
		}
	case *ReactiveSwitch:
		for index := range shape.Cases {
			if shape.Cases[index].Block != nil {
				pruner.walk(*shape.Cases[index].Block)
			}
		}
	case *ReactiveFor:
		pruner.walk(shape.Loop)
	case *ReactiveForOf:
		pruner.walk(shape.Loop)
	case *ReactiveForIn:
		pruner.walk(shape.Loop)
	case *ReactiveWhile:
		pruner.walk(shape.Loop)
	case *ReactiveDoWhile:
		pruner.walk(shape.Loop)
	case *ReactiveLabelTerminal:
		pruner.walk(shape.Block)
	case *ReactiveTry:
		pruner.walk(shape.Block)
		pruner.walk(shape.Handler)
	}
}
