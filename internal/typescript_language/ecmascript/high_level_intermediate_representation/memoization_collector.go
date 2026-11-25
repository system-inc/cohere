package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

type memoizationCollector struct {
	function     *Function
	dependencies *ScopeDependencies
	graph        *MemoizationGraph
	// definitions resolves a `LoadLocal` indirection, so a value read into a temporary is recorded
	// against the binding it came from rather than against the temporary.
	//
	// Upstream keeps the same map and consults it on every lvalue and rvalue. Without it a chain of
	// loads produces a chain of distinct nodes and the propagation stops at the first one.
	definitions map[static_single_assignment.DeclarationId]static_single_assignment.DeclarationId
	// scopeStack is the scopes currently open, innermost last.
	scopeStack []ScopeId
}

// resolve follows a `LoadLocal` indirection to the binding a value really names.
func (collector *memoizationCollector) resolve(
	declaration static_single_assignment.DeclarationId,
) static_single_assignment.DeclarationId {
	if target, found := collector.definitions[declaration]; found {
		return target
	}
	return declaration
}

func (collector *memoizationCollector) walk(block ReactiveBlock) {
	for _, statement := range block {
		switch shape := statement.(type) {
		case *ReactiveInstructionStatement:
			collector.visitInstruction(shape.Instruction)

		case *ReactiveScopeBlock:
			collector.scopeStack = append(collector.scopeStack, shape.Scope)
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
			for _, reassigned := range collector.dependencies.ReassignmentsOf(shape.Scope) {
				declaration := collector.resolve(declarationOf(collector.function, reassigned))
				collector.associateChain(declaration)
			}
			collector.walk(shape.Instructions)
			collector.scopeStack = collector.scopeStack[:len(collector.scopeStack)-1]

		case *ReactiveTerminalStatement:
			collector.visitTerminal(shape)
		}
	}
}

// visitInstruction records one instruction's lvalue, its level, and its operands as dependencies.
func (collector *memoizationCollector) visitInstruction(instruction *ReactiveInstruction) {
	if instruction == nil || instruction.Value == nil {
		return
	}
	level := MemoizationLevelOfReactiveValue(instruction.Value)

	// Places split by role, because an instruction's assignment targets are lvalues and only the
	// places it reads are dependencies. Collapsing the two inverts every store's edge: the binding
	// written to would become a dependency of the temporary rather than a value depending on what
	// was stored into it, and the propagation then walks away from the value it is looking for.
	var operands []static_single_assignment.DeclarationId
	var defines []Place
	collector.eachMemoizationOperand(instruction.Value, func(place Place, role PlaceRole) {
		if role == PlaceRoleDefine {
			defines = append(defines, place)
			return
		}
		operands = append(operands, collector.resolve(declarationOf(collector.function, place.Identifier)))
	})

	if instruction.LValue != nil {
		lvalue := collector.resolve(declarationOf(collector.function, instruction.LValue.Identifier))
		collector.graph.Record(lvalue, level, operands)
		collector.associate(lvalue)
	}

	// The assignment targets a value carries in addition to the instruction's own lvalue.
	//
	// Upstream returns `{lvalues, rvalues}` per kind, and seven kinds put a second place in
	// `lvalues`: `StoreLocal`, `StoreContext`, `DeclareLocal`, `DeclareContext`, `PrefixUpdate`,
	// `PostfixUpdate`, and every place bound by a `Destructure` pattern. Each carries its own level,
	// which is not the level of the instruction that produced it.
	for _, place := range defines {
		declaration := collector.resolve(declarationOf(collector.function, place.Identifier))
		collector.graph.Record(declaration, memoizationLevelOfDefine(instruction.Value, place), operands)
		collector.associate(declaration)
	}

	for _, operand := range operands {
		collector.graph.Declare(operand)
		collector.associate(operand)
	}

	// The `LoadLocal` indirection, recorded after the operands so this instruction still reads
	// through the previous definition rather than through the one it is establishing.
	plain, isPlain := instruction.Value.(*ReactiveInstructionValue)
	if !isPlain || plain.Value == nil || instruction.LValue == nil {
		return
	}
	if load, isLoad := plain.Value.(*LoadLocal); isLoad {
		collector.definitions[declarationOf(collector.function, instruction.LValue.Identifier)] =
			collector.resolve(declarationOf(collector.function, load.Place.Identifier))
		return
	}

	// A hook's arguments escape: React may retain them, which is why the closure passed to
	// `useEffect` is memoized. Upstream skips this for a `noAlias` hook; that consultation is
	// `MemoizationInputsGapCallSignatures` and is declined, so every hook argument escapes here.
	// The gap direction is conservative -- more values held, never fewer.
	switch call := plain.Value.(type) {
	case *CallExpression:
		if IsHookCallee(collector.function, call.Callee.Identifier) {
			for _, argument := range call.Arguments {
				collector.graph.MarkEscaping(
					collector.resolve(declarationOf(collector.function, argument.Place.Identifier)),
				)
			}
		}
	case *MethodCall:
		if IsHookCallee(collector.function, call.Property.Identifier) {
			for _, argument := range call.Arguments {
				collector.graph.MarkEscaping(
					collector.resolve(declarationOf(collector.function, argument.Place.Identifier)),
				)
			}
		}
	}
}

// associate records that a declaration belongs to the innermost scope currently open.
func (collector *memoizationCollector) associate(declaration static_single_assignment.DeclarationId) {
	if len(collector.scopeStack) == 0 || collector.dependencies == nil {
		return
	}
	collector.associateScope(declaration, collector.scopeStack[len(collector.scopeStack)-1])
}

// associateChain records that a declaration belongs to every scope currently open.
//
// Distinct from `associate`, which records only the innermost, and the difference is the point. An
// ordinary value belongs to the scope it was computed in. A value that escapes from inside a nest of
// scopes -- returned, or reassigned -- depends on all of them having been evaluated, so forcing it
// has to force the whole chain or the outer scopes' dependencies are never held.
func (collector *memoizationCollector) associateChain(declaration static_single_assignment.DeclarationId) {
	if collector.dependencies == nil {
		return
	}
	for _, scope := range collector.scopeStack {
		collector.associateScope(declaration, scope)
	}
}

// associateScope records that a declaration belongs to one named scope.
func (collector *memoizationCollector) associateScope(
	declaration static_single_assignment.DeclarationId,
	scope ScopeId,
) {
	var scopeDependencies []static_single_assignment.DeclarationId
	for _, dependency := range collector.dependencies.DependenciesOf(scope) {
		scopeDependencies = append(scopeDependencies,
			collector.resolve(declarationOf(collector.function, dependency.Identifier)))
	}
	collector.graph.AssociateScope(declaration, scope, scopeDependencies)
}

// visitTerminal descends, and records a returned value as escaping.
func (collector *memoizationCollector) visitTerminal(statement *ReactiveTerminalStatement) {
	switch shape := statement.Terminal.(type) {
	case *ReactiveReturn:
		returned := collector.resolve(declarationOf(collector.function, shape.Value.Identifier))
		collector.graph.MarkEscaping(returned)
		// A return inside scopes makes those scopes dependencies of the returned value, because they
		// have to be evaluated for the return to happen. Upstream's `CollectDependenciesVisitor`
		// arm for `ReturnTerminal`; without it the enclosing scopes' own dependencies are never
		// forced and a returned value can be held while what produced it is not.
		collector.associateChain(returned)
	case *ReactiveIf:
		collector.walk(shape.Consequent)
		if shape.Alternate != nil {
			collector.walk(*shape.Alternate)
		}
	case *ReactiveSwitch:
		for index := range shape.Cases {
			if shape.Cases[index].Block != nil {
				collector.walk(*shape.Cases[index].Block)
			}
		}
	case *ReactiveFor:
		collector.walk(shape.Loop)
	case *ReactiveForOf:
		collector.walk(shape.Loop)
	case *ReactiveForIn:
		collector.walk(shape.Loop)
	case *ReactiveWhile:
		collector.walk(shape.Loop)
	case *ReactiveDoWhile:
		collector.walk(shape.Loop)
	case *ReactiveLabelTerminal:
		collector.walk(shape.Block)
	case *ReactiveTry:
		collector.walk(shape.Block)
		collector.walk(shape.Handler)
	}
}

// eachMemoizationOperand visits every aliased operand of a reactive value, composites included,
// with its role.
//
// The role is carried rather than dropped because this pass reads it: a `PlaceRoleDefine` place is
// an assignment target and becomes an lvalue, and everything else is a dependency.
//
// A sequence's prefix holds real instructions even when the sequence is nested inside a logical,
// ternary, or optional value. Upstream's `computeMemoizationInputs(SequenceExpression)` visits each
// prefix instruction while recursively computing the enclosing value's inputs. Visiting sequences
// only when they are the top-level instruction loses allocations and `LoadLocal` definition edges
// in nested arms, so an escaping result can look non-escaping and its scope is pruned.
func (collector *memoizationCollector) eachMemoizationOperand(value ReactiveValue, visit func(Place, PlaceRole)) {
	switch shape := value.(type) {
	case *ReactiveInstructionValue:
		if shape.Value != nil {
			EachPlace(shape.Value, visit)
		}
	case *ReactiveLogicalValue:
		collector.eachMemoizationOperand(shape.Left, visit)
		collector.eachMemoizationOperand(shape.Right, visit)
	case *ReactiveTernaryValue:
		// Keep the test in this port's conservative alias set. Upstream excludes it, but changing
		// that policy is independent of reaching nested sequence instructions and needs its own
		// corpus trade-off.
		collector.eachMemoizationOperand(shape.Test, visit)
		collector.eachMemoizationOperand(shape.Consequent, visit)
		collector.eachMemoizationOperand(shape.Alternate, visit)
	case *ReactiveSequenceValue:
		for _, nested := range shape.Instructions {
			collector.visitInstruction(nested)
		}
		// Prefix instructions are graph nodes of their own. Only the sequence's final value aliases
		// the enclosing expression.
		collector.eachMemoizationOperand(shape.Value, visit)
	case *ReactiveOptionalValue:
		collector.eachMemoizationOperand(shape.Value, visit)
	}
}
