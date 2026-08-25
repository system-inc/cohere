// Pruning scopes whose values never escape, which is the reason the memoization lattice exists.
//
// This is React's `pruneNonEscapingScopes` (`ReactiveScopes/PruneNonEscapingScopes.ts` at
// `reactconformance.UpstreamSha`), assembled from the pieces this package already carries: the
// lattice in `memoization_level.go`, the per-kind classification in `memoization_inputs.go`, and the
// graph and propagation in `memoization_graph.go`.
//
// # What it does, in four phases
//
//  1. Walk the tree once, building a node per declaration: its memoization level, the operands it
//     depends on, and the scopes it belongs to. The same walk collects the escaping roots.
//  2. Roots are values that are returned, or passed as an argument to a hook. A returned value
//     escapes because the caller holds it; a hook argument escapes because React may retain it,
//     which is why the closure handed to `useEffect` is memoized.
//  3. Propagate outward from the roots. See `ComputeMemoized`.
//  4. Replace any scope with no memoized output by its own instructions, dropping the wrapper.
//
// # Why a scope with no memoized output is worth dropping
//
// Memoizing a value nothing holds costs a comparison every render and buys nothing. Upstream's
// worked example is a component building three arrays where only one is returned: without this pass
// all three are memoized, and two of those memoizations can never pay for themselves.
package hir

import (
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
)

// PruneNonEscapingScopesResult reports what the pass did, and what it could not decide.
type PruneNonEscapingScopesResult struct {
	// Pruned is how many scopes were replaced by their own instructions.
	Pruned int
	// Declarations is how many declarations the graph held.
	Declarations int
	// EscapingRoots is how many values were found to escape.
	EscapingRoots int
	// Memoized is how many declarations the propagation held.
	Memoized int
}

// PruneNonEscapingScopes drops scopes whose values are not held by anything.
//
// The checker is used for nothing today and is taken so the signature does not change when
// `MemoizationInputsGapCallSignatures` closes -- that gap needs a type-keyed lookup and the checker
// is what would answer it.
func PruneNonEscapingScopes(tree *ReactiveFunction, function *Function,
	dependencies *ScopeDependencies, typeChecker *shimchecker.Checker) PruneNonEscapingScopesResult {
	if tree == nil || function == nil {
		return PruneNonEscapingScopesResult{}
	}

	collector := memoizationCollector{
		function:     function,
		dependencies: dependencies,
		graph:        NewMemoizationGraph(),
		definitions:  map[DeclarationId]DeclarationId{},
	}
	for _, parameter := range function.Params {
		collector.graph.Declare(declarationOf(function, parameter.Identifier))
	}
	collector.walk(tree.Body)

	memoized := collector.graph.ComputeMemoized()

	pruned := prunePassOverScopes(tree, function, dependencies, memoized)

	return PruneNonEscapingScopesResult{
		Pruned:        pruned,
		Declarations:  collector.graph.Len(),
		EscapingRoots: collector.graph.EscapingCount(),
		Memoized:      len(memoized),
	}
}

type memoizationCollector struct {
	function     *Function
	dependencies *ScopeDependencies
	graph        *MemoizationGraph
	// definitions resolves a `LoadLocal` indirection, so a value read into a temporary is recorded
	// against the binding it came from rather than against the temporary.
	//
	// Upstream keeps the same map and consults it on every lvalue and rvalue. Without it a chain of
	// loads produces a chain of distinct nodes and the propagation stops at the first one.
	definitions map[DeclarationId]DeclarationId
	// scopeStack is the scopes currently open, innermost last.
	scopeStack []ScopeId
}

// resolve follows a `LoadLocal` indirection to the binding a value really names.
func (c *memoizationCollector) resolve(declaration DeclarationId) DeclarationId {
	if target, found := c.definitions[declaration]; found {
		return target
	}
	return declaration
}

func (c *memoizationCollector) walk(block ReactiveBlock) {
	for _, statement := range block {
		switch shape := statement.(type) {
		case *ReactiveInstructionStatement:
			c.visitInstruction(shape.Instruction)

		case *ReactiveScopeBlock:
			c.scopeStack = append(c.scopeStack, shape.Scope)
			c.walk(shape.Instructions)
			c.scopeStack = c.scopeStack[:len(c.scopeStack)-1]

		case *ReactiveTerminalStatement:
			c.visitTerminal(shape)
		}
	}
}

// visitInstruction records one instruction's lvalue, its level, and its operands as dependencies.
func (c *memoizationCollector) visitInstruction(instruction *ReactiveInstruction) {
	if instruction == nil || instruction.Value == nil {
		return
	}
	level := MemoizationLevelOfReactiveValue(instruction.Value)

	// A sequence carries instructions inside a value; they are instructions for this purpose and
	// missing them loses every declaration they make.
	if sequence, isSequence := instruction.Value.(*ReactiveSequenceValue); isSequence {
		for _, nested := range sequence.Instructions {
			c.visitInstruction(nested)
		}
	}

	var operands []DeclarationId
	c.eachOperand(instruction.Value, func(place Place) {
		operands = append(operands, c.resolve(declarationOf(c.function, place.Identifier)))
	})

	if instruction.LValue != nil {
		lvalue := c.resolve(declarationOf(c.function, instruction.LValue.Identifier))
		c.graph.Record(lvalue, level, operands)
		c.associate(lvalue)
	}
	for _, operand := range operands {
		c.graph.Declare(operand)
		c.associate(operand)
	}

	// The `LoadLocal` indirection, recorded after the operands so this instruction still reads
	// through the previous definition rather than through the one it is establishing.
	plain, isPlain := instruction.Value.(*ReactiveInstructionValue)
	if !isPlain || plain.Value == nil || instruction.LValue == nil {
		return
	}
	if load, isLoad := plain.Value.(*LoadLocal); isLoad {
		c.definitions[declarationOf(c.function, instruction.LValue.Identifier)] =
			c.resolve(declarationOf(c.function, load.Place.Identifier))
		return
	}

	// A hook's arguments escape: React may retain them, which is why the closure passed to
	// `useEffect` is memoized. Upstream skips this for a `noAlias` hook; that consultation is
	// `MemoizationInputsGapCallSignatures` and is declined, so every hook argument escapes here.
	// The gap direction is conservative -- more values held, never fewer.
	switch call := plain.Value.(type) {
	case *CallExpression:
		if IsHookCallee(c.function, call.Callee.Identifier) {
			for _, argument := range call.Args {
				c.graph.MarkEscaping(c.resolve(declarationOf(c.function, argument.Place.Identifier)))
			}
		}
	case *MethodCall:
		if IsHookCallee(c.function, call.Property.Identifier) {
			for _, argument := range call.Args {
				c.graph.MarkEscaping(c.resolve(declarationOf(c.function, argument.Place.Identifier)))
			}
		}
	}
}

// associate records that a declaration belongs to every scope currently open.
func (c *memoizationCollector) associate(declaration DeclarationId) {
	if len(c.scopeStack) == 0 || c.dependencies == nil {
		return
	}
	scope := c.scopeStack[len(c.scopeStack)-1]
	var scopeDependencies []DeclarationId
	for _, dependency := range c.dependencies.DependenciesOf(scope) {
		scopeDependencies = append(scopeDependencies,
			c.resolve(declarationOf(c.function, dependency.Identifier)))
	}
	c.graph.AssociateScope(declaration, scope, scopeDependencies)
}

// visitTerminal descends, and records a returned value as escaping.
func (c *memoizationCollector) visitTerminal(statement *ReactiveTerminalStatement) {
	switch shape := statement.Terminal.(type) {
	case *ReactiveReturn:
		c.graph.MarkEscaping(c.resolve(declarationOf(c.function, shape.Value.Identifier)))
	case *ReactiveIf:
		c.walk(shape.Consequent)
		if shape.Alternate != nil {
			c.walk(*shape.Alternate)
		}
	case *ReactiveSwitch:
		for index := range shape.Cases {
			if shape.Cases[index].Block != nil {
				c.walk(*shape.Cases[index].Block)
			}
		}
	case *ReactiveFor:
		c.walk(shape.Loop)
	case *ReactiveForOf:
		c.walk(shape.Loop)
	case *ReactiveForIn:
		c.walk(shape.Loop)
	case *ReactiveWhile:
		c.walk(shape.Loop)
	case *ReactiveDoWhile:
		c.walk(shape.Loop)
	case *ReactiveLabelTerminal:
		c.walk(shape.Block)
	case *ReactiveTry:
		c.walk(shape.Block)
		c.walk(shape.Handler)
	}
}

// eachOperand visits every place a reactive value names, composites included.
func (c *memoizationCollector) eachOperand(value ReactiveValue, visit func(Place)) {
	switch shape := value.(type) {
	case *ReactiveInstructionValue:
		if shape.Value != nil {
			EachPlace(shape.Value, func(place Place, role PlaceRole) { visit(place) })
		}
	case *ReactiveLogicalValue:
		c.eachOperand(shape.Left, visit)
		c.eachOperand(shape.Right, visit)
	case *ReactiveTernaryValue:
		c.eachOperand(shape.Test, visit)
		c.eachOperand(shape.Consequent, visit)
		c.eachOperand(shape.Alternate, visit)
	case *ReactiveSequenceValue:
		c.eachOperand(shape.Value, visit)
	case *ReactiveOptionalValue:
		c.eachOperand(shape.Value, visit)
	}
}

// prunePassOverScopes replaces every scope with no memoized output by its own instructions.
//
// Upstream keeps a scope whose declarations and reassignments are both empty, or which carries an
// early-return value: the memoized value is returned from inside it and `propagateEarlyReturns`
// needs the scope standing. We have no early-return representation, so only the first half applies
// and the second is why `PropagateEarlyReturns` is still open rather than declined.
func prunePassOverScopes(tree *ReactiveFunction, function *Function,
	dependencies *ScopeDependencies, memoized map[DeclarationId]bool) int {
	pruned := 0
	TransformReactiveFunction(tree, ReactiveTransformer{
		Scope: func(scope *ReactiveScopeBlock, traverse func()) ReactiveTransformed {
			traverse()
			if dependencies == nil {
				return KeepStatement()
			}
			declarations := dependencies.DeclarationsOf(scope.Scope)
			reassignments := dependencies.ReassignmentsOf(scope.Scope)
			if len(declarations) == 0 && len(reassignments) == 0 {
				return KeepStatement()
			}
			for _, declared := range declarations {
				if memoized[declarationOf(function, declared)] {
					return KeepStatement()
				}
			}
			for _, reassigned := range reassignments {
				if memoized[declarationOf(function, reassigned)] {
					return KeepStatement()
				}
			}
			pruned++
			return ReplaceStatements(scope.Instructions)
		},
	})
	return pruned
}
