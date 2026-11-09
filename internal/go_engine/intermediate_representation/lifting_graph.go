package intermediate_representation

// The Go adapter for the shared core (#ee33njz, go-ir-shape.md section 2): static_single_assignment.Graph over our
// Function and BasicBlock, so Braun's construction lifts what go/ir's lift.go lifts.
//
// A lifted Allocation is a declaration. The Allocation itself defines its zero value, a Store to it defines the
// stored operand, and a Load from it uses the value that reaches it. No other instruction has a place. Edges are
// all real, in Successors order; nothing is a context store, and there is no Returns place. The Recover block is a
// second root with no predecessors, placed after every block the entry reaches; lifting never asks it for a
// value, since go/ir lifts no result of a function that has one and the Recover block reads nothing else.

import (
	"fmt"

	"github.com/system-inc/cohere/static_single_assignment"
)

// liftingGraph is the adapter. It holds nothing; each call reads the liftingFunction it is handed.
type liftingGraph struct{}

// liftingPlace is a place to the core: the identifier of the value a lifted instruction defines or uses.
type liftingPlace struct {
	identifier static_single_assignment.IdentifierId
}

// liftingSlot is the one place a lifted Allocation, Store, Load or debug reference has, kept by the instruction's
// index in its block. isPlace says whether the instruction has one: the zero slot has none, kept apart from role,
// since the core's Use is its Role's zero value.
type liftingSlot struct {
	place   liftingPlace
	role    static_single_assignment.Role
	isPlace bool

	// deleted is whether the round drops the instruction once lifted.
	deleted bool

	// isCell is whether the instruction is a lifted Allocation, the cell a declaration names.
	isCell bool
}

// liftingBlock is one block as the core sees it: the block, the phis the core places in it until lifting turns
// them into *Phi instructions, and its instructions' places by index, nil when none of them has one.
type liftingBlock struct {
	block *BasicBlock
	phis  []*static_single_assignment.Phi[liftingPlace]
	slots []liftingSlot

	// newPhis are the *Phi instructions the resolve made of the core's phis here, to prepend.
	newPhis []*Phi
}

// liftingFunction is one function as the core sees it for one round of lifting.
type liftingFunction struct {
	function *Function

	// blocks are the function's blocks as the core sees them, by index.
	blocks []*liftingBlock

	// order is the blocks in reverse postorder from the entry over Successors, then those the entry doesn't
	// reach (the Recover block), in index order. Braun's construction walks it.
	order []*liftingBlock

	// declarations are each identifier's declaration, by identifier: the first len(allocations) are the
	// declarations' own, and Mint appends.
	declarations []static_single_assignment.DeclarationId

	// allocations are the lifted Allocations, by declaration.
	allocations []*Allocation
}

var _ static_single_assignment.Graph[*liftingFunction, *liftingBlock, liftingPlace] = liftingGraph{}

func (liftingGraph) Entry(*liftingFunction) static_single_assignment.BlockId { return 0 }

func (liftingGraph) BlockBound(lifting *liftingFunction) int { return len(lifting.function.Blocks) }

func (liftingGraph) Block(lifting *liftingFunction, id static_single_assignment.BlockId) (*liftingBlock, bool) {
	if int(id) >= len(lifting.blocks) {
		return nil, false
	}
	return lifting.blocks[id], true
}

func (liftingGraph) Blocks(lifting *liftingFunction) []*liftingBlock { return lifting.order }

// SetBlocks replaces the order the core walks.
//
// Mutates: lifting the Graph contract has the core set the block order on the function it is handed
func (liftingGraph) SetBlocks(lifting *liftingFunction, blocks []*liftingBlock) {
	lifting.order = blocks
}

// Retain is asked only by the core's graph maintenance, which lifting doesn't run: the builder's block
// optimization has already removed every block the entry doesn't reach but the Recover block.
func (liftingGraph) Retain(*liftingFunction, func(static_single_assignment.BlockId) bool) {
	// Lifting drops no block, so there is nothing to forget.
}

// Placeholder is asked only of an IR that reports Fallthrough edges, which ours never does.
func (liftingGraph) Placeholder(_ *liftingFunction, block *liftingBlock) *liftingBlock { return block }

func (liftingGraph) Id(block *liftingBlock) static_single_assignment.BlockId {
	return static_single_assignment.BlockId(block.block.Index)
}

func (liftingGraph) Predecessors(block *liftingBlock) []static_single_assignment.BlockId {
	predecessors := make([]static_single_assignment.BlockId, len(block.block.Predecessors))
	for index, predecessor := range block.block.Predecessors {
		predecessors[index] = static_single_assignment.BlockId(predecessor.Index)
	}
	return predecessors
}

// SetPredecessors is asked only by the core's graph maintenance, which lifting doesn't run; the builder keeps
// Predecessors itself.
func (liftingGraph) SetPredecessors(*liftingBlock, []static_single_assignment.BlockId) {
	// The builder keeps Predecessors, and lifting changes no edge.
}

func (liftingGraph) Phis(block *liftingBlock) []*static_single_assignment.Phi[liftingPlace] {
	return block.phis
}

// SetPhis replaces the core's phis in block.
//
// Mutates: block the Graph contract has the core keep its phis on the block it is handed
func (liftingGraph) SetPhis(block *liftingBlock, phis []*static_single_assignment.Phi[liftingPlace]) {
	block.phis = phis
}

func (liftingGraph) EachEdge(block *liftingBlock, visit func(static_single_assignment.BlockId,
	static_single_assignment.Edge)) {
	for _, successor := range block.block.Successors {
		visit(static_single_assignment.BlockId(successor.Index), static_single_assignment.Real)
	}
}

func (liftingGraph) EndsInReturn(block *liftingBlock) bool {
	instructions := block.block.Instructions
	if len(instructions) == 0 {
		return false
	}
	_, isReturn := instructions[len(instructions)-1].(*Return)
	return isReturn
}

func (liftingGraph) InstructionCount(_ *liftingFunction, block *liftingBlock) int {
	return len(block.block.Instructions)
}

func (liftingGraph) EachInstructionPlace(lifting *liftingFunction, block *liftingBlock, index int,
	visit func(*liftingPlace, static_single_assignment.Role)) {
	if index < len(block.slots) && block.slots[index].isPlace {
		visit(&block.slots[index].place, block.slots[index].role)
	}
}

func (liftingGraph) IsContextStore(*liftingFunction, *liftingBlock, int) bool { return false }

func (liftingGraph) ContextStoreDefines(*liftingFunction, *liftingBlock, int, liftingPlace) bool {
	return false
}

// SetInstructionOrder is the core's evaluation order, which lifting doesn't ask for.
func (liftingGraph) SetInstructionOrder(*liftingFunction, *liftingBlock, int,
	static_single_assignment.EvaluationOrder) {
	// Lifting asks for no evaluation order, so there is none to record.
}

// EachTerminalPlace visits nothing: a block's terminal is its last instruction, and EachInstructionPlace has
// already visited it, placeless as every terminal is.
func (liftingGraph) EachTerminalPlace(*liftingBlock, func(*liftingPlace, static_single_assignment.Role)) {
	// The terminal is the last instruction, already visited, and has no place.
}

func (liftingGraph) SetTerminalOrder(*liftingBlock, static_single_assignment.EvaluationOrder) {
	// Lifting asks for no evaluation order, so there is none to record.
}

// Parameters are none: a parameter is never lifted, only the Allocation its value is spilled to.
func (liftingGraph) Parameters(*liftingFunction) []liftingPlace { return nil }

func (liftingGraph) Returns(*liftingFunction) *liftingPlace { return nil }

func (liftingGraph) Declaration(lifting *liftingFunction,
	identifier static_single_assignment.IdentifierId) static_single_assignment.DeclarationId {
	return lifting.declarations[identifier]
}

func (liftingGraph) Contextual(*liftingFunction, static_single_assignment.DeclarationId) bool {
	return false
}

// Mint makes a new identifier of original's declaration.
//
// Mutates: lifting the Graph contract has the core record each value it mints on the function it is handed
func (liftingGraph) Mint(lifting *liftingFunction,
	original static_single_assignment.IdentifierId) static_single_assignment.IdentifierId {
	lifting.declarations = append(lifting.declarations, lifting.declarations[original])
	return static_single_assignment.IdentifierId(len(lifting.declarations) - 1)
}

func (liftingGraph) Named(*liftingFunction, static_single_assignment.IdentifierId) bool { return true }

func (liftingGraph) PlaceString(lifting *liftingFunction, identifier static_single_assignment.IdentifierId) string {
	allocation := lifting.allocations[lifting.declarations[identifier]]
	return fmt.Sprintf("%s#%d (%s)", allocation.comment, identifier, allocation.Type())
}

func (liftingGraph) IdentifierOf(place liftingPlace) static_single_assignment.IdentifierId {
	return place.identifier
}

func (liftingGraph) WithIdentifier(_ liftingPlace, identifier static_single_assignment.IdentifierId) liftingPlace {
	return liftingPlace{identifier: identifier}
}
