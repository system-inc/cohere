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
			// A variable reassigned inside a scope takes the whole chain of enclosing scopes as
			// scopes of its own, this one included.
			//
			// Upstream's `CollectDependenciesVisitor.visitScope`. Without it a reassigned value that
			// escapes does not force the scopes whose evaluation produced the new value, so their
			// dependencies go unmemoized and the reassignment is invisible to the propagation.
			//
			// Measured inert today, and recorded as inert rather than described by what it is for:
			// on `error.invalid-useCallback-captures-reassigned-context` the lowering produces a
			// `StoreLocal` with `InstructionKindReassign`, but `ScopeDependencies.ReassignmentsOf`
			// returns empty for every scope in that program, so this loop has nothing to iterate.
			// The gate is `checkValidDependency`, one stage upstream in dependency collection, and
			// it is a faithful transcription of upstream's own gate -- so the divergence is further
			// up still and is not this pass's to fix. Filed as its own question rather than worked
			// around here, because making this arm fire by loosening a gate it does not own would
			// be fixture-shaped development.
			for _, reassigned := range c.dependencies.ReassignmentsOf(shape.Scope) {
				declaration := c.resolve(declarationOf(c.function, reassigned))
				c.associateChain(declaration)
			}
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

	// Places split by role, because an instruction's assignment targets are lvalues and only the
	// places it reads are dependencies. Collapsing the two inverts every store's edge: the binding
	// written to would become a dependency of the temporary rather than a value depending on what
	// was stored into it, and the propagation then walks away from the value it is looking for.
	var operands []DeclarationId
	var defines []Place
	c.eachOperand(instruction.Value, func(place Place, role PlaceRole) {
		if role == PlaceRoleDefine {
			defines = append(defines, place)
			return
		}
		operands = append(operands, c.resolve(declarationOf(c.function, place.Identifier)))
	})

	if instruction.LValue != nil {
		lvalue := c.resolve(declarationOf(c.function, instruction.LValue.Identifier))
		c.graph.Record(lvalue, level, operands)
		c.associate(lvalue)
	}

	// The assignment targets a value carries in addition to the instruction's own lvalue.
	//
	// Upstream returns `{lvalues, rvalues}` per kind, and seven kinds put a second place in
	// `lvalues`: `StoreLocal`, `StoreContext`, `DeclareLocal`, `DeclareContext`, `PrefixUpdate`,
	// `PostfixUpdate`, and every place bound by a `Destructure` pattern. Each carries its own level,
	// which is not the level of the instruction that produced it.
	for _, place := range defines {
		declaration := c.resolve(declarationOf(c.function, place.Identifier))
		c.graph.Record(declaration, memoizationLevelOfDefine(instruction.Value, place), operands)
		c.associate(declaration)
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

// associate records that a declaration belongs to the innermost scope currently open.
func (c *memoizationCollector) associate(declaration DeclarationId) {
	if len(c.scopeStack) == 0 || c.dependencies == nil {
		return
	}
	c.associateScope(declaration, c.scopeStack[len(c.scopeStack)-1])
}

// associateChain records that a declaration belongs to every scope currently open.
//
// Distinct from `associate`, which records only the innermost, and the difference is the point. An
// ordinary value belongs to the scope it was computed in. A value that escapes from inside a nest of
// scopes -- returned, or reassigned -- depends on all of them having been evaluated, so forcing it
// has to force the whole chain or the outer scopes' dependencies are never held.
func (c *memoizationCollector) associateChain(declaration DeclarationId) {
	if c.dependencies == nil {
		return
	}
	for _, scope := range c.scopeStack {
		c.associateScope(declaration, scope)
	}
}

// associateScope records that a declaration belongs to one named scope.
func (c *memoizationCollector) associateScope(declaration DeclarationId, scope ScopeId) {
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
		returned := c.resolve(declarationOf(c.function, shape.Value.Identifier))
		c.graph.MarkEscaping(returned)
		// A return inside scopes makes those scopes dependencies of the returned value, because they
		// have to be evaluated for the return to happen. Upstream's `CollectDependenciesVisitor`
		// arm for `ReturnTerminal`; without it the enclosing scopes' own dependencies are never
		// forced and a returned value can be held while what produced it is not.
		c.associateChain(returned)
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

// memoizationLevelOfDefine returns the level upstream assigns to an assignment target.
//
// It is deliberately not `MemoizationLevelOf` of the enclosing value. Upstream's classification
// returns a list of lvalues each carrying its own level, and for five of the seven kinds the target
// and the instruction's own lvalue are given *different* levels -- a `DeclareContext` writes a
// binding at `Memoized` while its temporary is `Unmemoized`. Reusing the value's level would flatten
// that distinction in the direction that holds too much, which costs granularity silently.
//
// The default is `Conditional` rather than `Never`, because every remaining way a place is defined
// is an indirection: it is memoized exactly when what flows into it is. `Never` would terminate the
// propagation at an assignment, which is the failure this function exists to prevent.
func memoizationLevelOfDefine(value ReactiveValue, place Place) MemoizationLevel {
	plain, isPlain := value.(*ReactiveInstructionValue)
	if !isPlain || plain.Value == nil {
		return MemoizationConditional
	}
	switch shape := plain.Value.(type) {
	// A context binding outlives the instruction that wrote it, so it is never pruned.
	case *DeclareContext, *StoreContext:
		return MemoizationMemoized

	// Declared and not held: not comparable with `Object.is`, and left alone unless forced.
	case *DeclareLocal:
		return MemoizationUnmemoized

	// A destructured pattern binds at `Conditional`, except a rest element, which allocates a fresh
	// object or array every evaluation and so must be held on its own account.
	case *Destructure:
		if isRestPlaceOfPattern(shape.LValue, place) {
			return MemoizationMemoized
		}
		return MemoizationConditional

	default:
		return MemoizationConditional
	}
}

// isRestPlaceOfPattern reports whether a place is a pattern's rest element.
//
// Upstream's `computePatternLValues` reaches `Memoized` for `Spread` in an array pattern and for a
// non-`ObjectProperty` in an object pattern, which are the same thing this tree spells as `Rest`.
func isRestPlaceOfPattern(pattern Pattern, place Place) bool {
	switch shape := pattern.(type) {
	case *ObjectPattern:
		if shape.Rest != nil && shape.Rest.Identifier == place.Identifier {
			return true
		}
		for _, property := range shape.Properties {
			if isRestPlaceOfPattern(property.Value, place) {
				return true
			}
		}
	case *ArrayPattern:
		if shape.Rest != nil && shape.Rest.Identifier == place.Identifier {
			return true
		}
		for _, element := range shape.Elements {
			if element.Value != nil && isRestPlaceOfPattern(element.Value, place) {
				return true
			}
		}
	}
	return false
}

// eachOperand visits every place a reactive value names, composites included, with its role.
//
// The role is carried rather than dropped because this pass reads it: a `PlaceRoleDefine` place is
// an assignment target and becomes an lvalue, and everything else is a dependency.
func (c *memoizationCollector) eachOperand(value ReactiveValue, visit func(Place, PlaceRole)) {
	switch shape := value.(type) {
	case *ReactiveInstructionValue:
		if shape.Value != nil {
			EachPlace(shape.Value, visit)
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
