package high_level_intermediate_representation

import (
	"github.com/system-inc/cohere/mutation_aliasing"
	"github.com/system-inc/cohere/static_single_assignment"
)

// rangeGraph is this IR's side of mutation_aliasing.Graph: ssaGraph's answers for single assignment,
// and this IR's answer at each seam where it differs from Adamic's.
//
// It carries the effect table the pass reads, and the values derived from a useRef result, which
// React's closure rule reads. A zero rangeGraph answers everything but Effects and Closure, which is
// all mutation_aliasing.BlockFirstOrder asks.
type rangeGraph struct {
	ssaGraph
	effects *AliasingEffects
	refs    map[static_single_assignment.IdentifierId]bool
}

func (rangeGraph) InstructionOrder(
	function *Function,
	block *BasicBlock,
	index int,
) (static_single_assignment.EvaluationOrder, bool) {
	instruction := function.Instructions[block.Instructions[index]]
	if instruction == nil {
		return 0, false
	}
	return instruction.Order, true
}

func (rangeGraph) TerminalOrder(block *BasicBlock) static_single_assignment.EvaluationOrder {
	return TerminalOrder(block.Terminal)
}

func (graph rangeGraph) Effects(function *Function, block *BasicBlock, index int) []AliasingEffect {
	return graph.effects.Get(block.Instructions[index])
}

// ParametersFrozen is React's rule: a component's or a hook's parameters are Frozen, and only a nested
// function expression's are Mutable.
func (rangeGraph) ParametersFrozen(function *Function) bool {
	return function.Kind == FunctionKindComponent || function.Kind == FunctionKindHook
}

func (rangeGraph) Context(function *Function) []Place { return function.Context }

func (rangeGraph) ReturnValue(block *BasicBlock) (Place, bool) {
	returnTerminal, ok := block.Terminal.(*Return)
	if !ok {
		return Place{}, false
	}
	return returnTerminal.Value, true
}

func (rangeGraph) StoredContextValue(
	function *Function,
	block *BasicBlock,
	index int,
) (static_single_assignment.IdentifierId, bool) {
	store, ok := function.Instructions[block.Instructions[index]].Value.(*StoreContext)
	if !ok {
		return 0, false
	}
	return store.Value.Identifier, true
}

// Closure is React's rule for a FunctionExpression: created Frozen when every capture is immutable,
// none is derived from a useRef result, and the closure's body only reads them
// (hasReadOnlyClosureEffectsForCaptures).
func (graph rangeGraph) Closure(
	function *Function,
	block *BasicBlock,
	index int,
	into static_single_assignment.IdentifierId,
	kinds map[static_single_assignment.IdentifierId]mutation_aliasing.EffectValueKind,
) ([]Place, bool) {
	instruction := function.Instructions[block.Instructions[index]]
	expression, ok := instruction.Value.(*FunctionExpression)
	if !ok || into != instruction.LValue.Identifier {
		return nil, false
	}
	allImmutable := len(expression.Captures) > 0
	for _, capture := range expression.Captures {
		_, immutable := kinds[capture.Identifier]
		allImmutable = allImmutable && immutable && !graph.refs[capture.Identifier]
	}
	frozen := allImmutable && int(expression.Function) < len(function.Functions) &&
		hasReadOnlyClosureEffectsForCaptures(function.Functions[expression.Function], expression.Captures, kinds)
	return expression.Captures, frozen
}
