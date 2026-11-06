package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// ssaGraph is this IR's side of static_single_assignment.Graph.
//
// Where the two IRs that share the module differ, this is cohere's answer: a structured terminal's
// fallthrough is reported as one, MaybeThrow's handler as an exceptional edge, a StoreContext as a
// context store, and the Returns place is the function's own. Each method is a field read or the
// visitor this package already has, so the passes see exactly what they read before they moved.
type ssaGraph struct{}

func (ssaGraph) Entry(function *Function) static_single_assignment.BlockId { return function.Entry }

func (ssaGraph) BlockBound(function *Function) int {
	// Ids are bounded by `nextBlock`, and by the largest id in the table for a function built by
	// hand without `NewFunction`.
	bound := int(function.nextBlock)
	for id := range function.blocksById {
		if int(id) >= bound {
			bound = int(id) + 1
		}
	}
	return bound
}

func (ssaGraph) Block(function *Function, id static_single_assignment.BlockId) (*BasicBlock, bool) {
	return function.Block(id)
}

func (ssaGraph) Blocks(function *Function) []*BasicBlock { return function.Blocks }

// SetBlocks writes a pass's reordered block list back.
//
// Mutates: function its block list is replaced, since a setter is how the module writes this IR
func (ssaGraph) SetBlocks(function *Function, blocks []*BasicBlock) { function.Blocks = blocks }

func (ssaGraph) Retain(function *Function, keep func(id static_single_assignment.BlockId) bool) {
	for id := range function.blocksById {
		if !keep(id) {
			delete(function.blocksById, id)
		}
	}
}

func (ssaGraph) Placeholder(function *Function, block *BasicBlock) *BasicBlock {
	placeholder := &BasicBlock{
		Id:           block.Id,
		Kind:         block.Kind,
		Terminal:     &Unreachable{},
		Predecessors: append([]static_single_assignment.BlockId(nil), block.Predecessors...),
	}
	function.blocksById[block.Id] = placeholder
	return placeholder
}

func (ssaGraph) Id(block *BasicBlock) static_single_assignment.BlockId { return block.Id }

func (ssaGraph) Predecessors(block *BasicBlock) []static_single_assignment.BlockId {
	return block.Predecessors
}

// SetPredecessors writes a block's recomputed predecessors back.
//
// Mutates: block its predecessors are replaced, since a setter is how the module writes this IR
func (ssaGraph) SetPredecessors(block *BasicBlock, predecessors []static_single_assignment.BlockId) {
	block.Predecessors = predecessors
}

func (ssaGraph) Phis(block *BasicBlock) []*Phi { return block.Phis }

// SetPhis writes the phis construction placed at a block back.
//
// Mutates: block its phis are replaced, since a setter is how the module writes this IR
func (ssaGraph) SetPhis(block *BasicBlock, phis []*Phi) { block.Phis = phis }

// EachEdge reports the structural fallthrough first, then EachSuccessor's real edges in its order,
// with MaybeThrow's handler marked exceptional. Try's handler is a real edge: it leaves the block
// holding the Try, before the try body runs, so nothing in that block can throw on the way.
func (ssaGraph) EachEdge(
	block *BasicBlock,
	visit func(successor static_single_assignment.BlockId, edge static_single_assignment.Edge),
) {
	if fallthroughBlock, ok := Fallthrough(block.Terminal); ok {
		visit(fallthroughBlock, static_single_assignment.Fallthrough)
	}
	handler := InvalidBlock
	if maybeThrow, ok := block.Terminal.(*MaybeThrow); ok {
		handler = maybeThrow.Handler
	}
	EachSuccessor(block.Terminal, func(successor static_single_assignment.BlockId) {
		if HasBlock(handler) && successor == handler {
			visit(successor, static_single_assignment.Exceptional)
			return
		}
		visit(successor, static_single_assignment.Real)
	})
}

func (ssaGraph) EndsInReturn(block *BasicBlock) bool {
	_, returns := block.Terminal.(*Return)
	return returns
}

func (ssaGraph) InstructionCount(function *Function, block *BasicBlock) int {
	return len(block.Instructions)
}

func (ssaGraph) EachInstructionPlace(function *Function, block *BasicBlock, index int,
	visit func(place *Place, role static_single_assignment.Role)) {
	EachInstructionPlacePointer(function.Instructions[block.Instructions[index]], func(place *Place, role PlaceRole) {
		visit(place, ssaRole(role))
	})
}

func (ssaGraph) IsContextStore(function *Function, block *BasicBlock, index int) bool {
	_, isContextStore := function.Instructions[block.Instructions[index]].Value.(*StoreContext)
	return isContextStore
}

func (ssaGraph) ContextStoreDefines(function *Function, block *BasicBlock, index int, place Place) bool {
	return place.Identifier == function.Instructions[block.Instructions[index]].LValue.Identifier
}

func (ssaGraph) SetInstructionOrder(
	function *Function,
	block *BasicBlock,
	index int,
	order static_single_assignment.EvaluationOrder,
) {
	function.Instructions[block.Instructions[index]].Order = order
}

func (ssaGraph) EachTerminalPlace(block *BasicBlock, visit func(place *Place, role static_single_assignment.Role)) {
	EachTerminalPlacePointer(block.Terminal, func(place *Place, role PlaceRole) {
		visit(place, ssaRole(role))
	})
}

func (ssaGraph) SetTerminalOrder(block *BasicBlock, order static_single_assignment.EvaluationOrder) {
	setTerminalOrder(block.Terminal, order)
}

func (ssaGraph) Params(function *Function) []Place { return function.Parameters }

func (ssaGraph) Returns(function *Function) *Place { return &function.Returns }

func (ssaGraph) Declaration(
	function *Function,
	identifier static_single_assignment.IdentifierId,
) static_single_assignment.DeclarationId {
	return function.Identifiers[identifier].Declaration
}

func (ssaGraph) Contextual(function *Function, declaration static_single_assignment.DeclarationId) bool {
	return function.ContextDeclarations[declaration]
}

// Mint creates a new value carrying the same source binding as the original.
//
// The DeclarationId is preserved, which is the whole point: after renaming, "which variable is
// this" is answered by the declaration and "which value is this" by the identifier, and a pass can
// ask either.
func (ssaGraph) Mint(
	function *Function,
	original static_single_assignment.IdentifierId,
) static_single_assignment.IdentifierId {
	source := function.Identifiers[original]
	return function.NewIdentifier(source.Name, source.Node, source.Declaration).Id
}

func (ssaGraph) Named(function *Function, identifier static_single_assignment.IdentifierId) bool {
	return function.Identifiers[identifier].Name != ""
}

func (ssaGraph) PlaceString(function *Function, identifier static_single_assignment.IdentifierId) string {
	return function.PlaceString(Place{Identifier: identifier})
}

func (ssaGraph) IdentifierOf(place Place) static_single_assignment.IdentifierId {
	return place.Identifier
}

// WithIdentifier keeps the place's effect, reactivity and range, which is what every renamed place
// and phi operand carried before the passes moved.
func (ssaGraph) WithIdentifier(place Place, identifier static_single_assignment.IdentifierId) Place {
	place.Identifier = identifier
	return place
}
