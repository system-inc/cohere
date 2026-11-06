package high_level_intermediate_representation

type scopePruner struct {
	dependencies *ScopeDependencies
	pruned       int
}

// walk visits a block, pruning any scope in it, and reports whether it held a return statement.
//
// The return flag propagates outward: a `return` anywhere inside a scope keeps that scope, and a
// scope's own walk must therefore see returns from every block nested within it.
func (pruner *scopePruner) walk(block ReactiveBlock) bool {
	sawReturn := false
	for _, statement := range block {
		switch shape := statement.(type) {
		case *ReactiveScopeBlock:
			// The nested walk's return flag decides this scope, and also propagates outward: a
			// return inside a nested scope is still a return inside this block.
			if pruner.visitScope(shape) {
				sawReturn = true
			}

		case *ReactiveTerminalStatement:
			if pruner.walkTerminal(shape) {
				sawReturn = true
			}
			if _, isReturn := shape.Terminal.(*ReactiveReturn); isReturn {
				sawReturn = true
			}
		}
	}
	return sawReturn
}

// visitScope prunes one scope if it qualifies, and reports whether it contained a return.
//
// Mutates: scope the pruning marks the caller's scope block pruned where it sits in the reactive tree
func (pruner *scopePruner) visitScope(scope *ReactiveScopeBlock) bool {
	sawReturn := pruner.walk(scope.Instructions)
	if scope.Pruned {
		return sawReturn
	}
	if sawReturn {
		return sawReturn
	}
	if len(pruner.dependencies.ReassignmentsOf(scope.Scope)) != 0 {
		return sawReturn
	}
	if pruner.hasOwnDeclaration(scope) {
		return sawReturn
	}
	scope.Pruned = true
	pruner.pruned++
	return sawReturn
}

// hasOwnDeclaration reports whether a scope declares any value that originated in the scope itself.
//
// Upstream's predicate of the same name, and the whole reason `ScopeDependencies` records an origin.
// A scope holding only declarations that bubbled up from nested scopes has produced nothing, so
// memoizing it buys nothing that memoizing the inner scopes does not already buy.
//
// A declaration with no recorded origin counts as the scope's own, which is the conservative
// direction: an unknown origin keeps the scope rather than pruning it on missing information.
func (pruner *scopePruner) hasOwnDeclaration(scope *ReactiveScopeBlock) bool {
	for _, declared := range pruner.dependencies.DeclarationsOf(scope.Scope) {
		origin, found := pruner.dependencies.OriginOf(declared)
		if !found || origin == scope.Scope {
			return true
		}
	}
	return false
}

// walkTerminal descends into every block a terminal contains, reporting whether any held a return.
func (pruner *scopePruner) walkTerminal(statement *ReactiveTerminalStatement) bool {
	sawReturn := false
	visit := func(block ReactiveBlock) {
		if pruner.walk(block) {
			sawReturn = true
		}
	}
	switch shape := statement.Terminal.(type) {
	case *ReactiveIf:
		visit(shape.Consequent)
		if shape.Alternate != nil {
			visit(*shape.Alternate)
		}
	case *ReactiveSwitch:
		for index := range shape.Cases {
			if shape.Cases[index].Block != nil {
				visit(*shape.Cases[index].Block)
			}
		}
	case *ReactiveFor:
		visit(shape.Loop)
	case *ReactiveForOf:
		visit(shape.Loop)
	case *ReactiveForIn:
		visit(shape.Loop)
	case *ReactiveWhile:
		visit(shape.Loop)
	case *ReactiveDoWhile:
		visit(shape.Loop)
	case *ReactiveLabelTerminal:
		visit(shape.Block)
	case *ReactiveTry:
		visit(shape.Block)
		visit(shape.Handler)
	}
	return sawReturn
}
