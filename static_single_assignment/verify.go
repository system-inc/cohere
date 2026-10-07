// Verification of single static assignment form: the property, checked rather than asserted.
//
// `Construct` is only useful if what it produces is actually SSA, and "the tests pass" is weak
// evidence when the tests were written by the same person as the construction. So the defining
// property is checked directly, over whatever function is handed in:
//
//  1. Every value is defined at most once.
//  2. EVERY USE IS DOMINATED BY ITS DEFINITION.
//
// The second is the real one. It is what makes SSA worth having: a pass that sees a use can reach
// the single definition and know it has already executed. A construction that places a phi at the
// wrong block, or renames a use to a definition sitting on a sibling branch, breaks exactly this
// and breaks nothing that a structural well-formedness check would notice.
//
// # Phi operands are checked against the PREDECESSOR, not the phi's own block
//
// A phi is not an ordinary instruction. Its operand for predecessor P is read on the edge from P,
// so the definition must dominate P's exit, not the block holding the phi. Checking it the naive
// way reports a false failure on every correct loop phi, since the back edge's value is defined
// below the header. This distinction is the single easiest thing to get wrong in a checker of this
// kind, and getting it wrong produces a checker that rejects correct code, which is at least loud.
// The reverse mistake - checking phis as though they were instructions in the predecessor - is the
// dangerous one, and it is why this is spelled out.
//
// # Dominance is computed here, over this graph
//
// `internal/utilities/control_flow_graph.AnalyzeDominators` is generic over that package's `Graph[E]`,
// not over an IR's function, so it cannot be pointed at this. Rather than materialise a parallel
// graph to borrow it, the same Cooper-Harvey-Kennedy fixed point is run here, over a reverse
// postorder of the real edges that ComputeDominance makes for itself (it says why the block slice
// isn't one). Being a second implementation is acceptable for a checker: a checker that shares an
// implementation with the thing it checks can agree with it and both be wrong.
package static_single_assignment

import "fmt"

// SSAViolation is one broken invariant.
type SSAViolation struct {
	// Kind is which invariant broke.
	Kind SSAViolationKind
	// Identifier is the value involved.
	Identifier IdentifierId
	// Block is where the violating use or duplicate definition sits.
	Block BlockId
	// Detail is a human-readable explanation naming both ends.
	Detail string
}

func (v SSAViolation) String() string { return v.Detail }

// SSAViolationKind is which invariant a violation breaks: one of the two single assignment states, or
// the precondition Construct rests on.
type SSAViolationKind uint8

const (
	// SSAViolationMultipleDefinitions is a value written more than once.
	SSAViolationMultipleDefinitions SSAViolationKind = iota
	// SSAViolationUseNotDominated is a use the definition does not dominate.
	SSAViolationUseNotDominated
	// SSAViolationEntryHasPredecessors is an entry block some edge enters, which Graph.Entry rules out
	// and Construct refuses.
	SSAViolationEntryHasPredecessors
)

// VerifySSA checks that a function is in single static assignment form and returns every violation.
//
// An empty result means the property holds. It does NOT mean the function was worth checking: a
// function whose variable references all lowered to globals has almost nothing to check and passes
// trivially. `CollectSSAStats` is how a caller tells a real pass from a vacuous one.
//
// Nested functions are not descended into; call it per function.
func VerifySSA[G Graph[F, B, P], F any, B comparable, P any](graph G, function F) []SSAViolation {
	blocks := graph.Blocks(function)
	if len(blocks) == 0 {
		return nil
	}

	var violations []SSAViolation
	if entry, predecessors, entered := entryPredecessors(graph, function); entered {
		violations = append(violations, SSAViolation{
			Kind:  SSAViolationEntryHasPredecessors,
			Block: entry,
			Detail: fmt.Sprintf("the entry block bb%d has predecessors %v, and no edge may enter it",
				entry, predecessors),
		})
	}
	dominance := ComputeDominance(graph, function)

	// Where each value is defined. A parameter is defined at the entry block.
	definedIn := map[IdentifierId]BlockId{}
	// Position within the block, so a use earlier in the same block than its definition is caught.
	definedAt := map[IdentifierId]int{}

	recordDefinition := func(id IdentifierId, blockId BlockId, position int) {
		if previous, exists := definedIn[id]; exists {
			violations = append(violations, SSAViolation{
				Kind:       SSAViolationMultipleDefinitions,
				Identifier: id,
				Block:      blockId,
				Detail: fmt.Sprintf("value %s is defined in bb%d and again in bb%d",
					graph.PlaceString(function, id), previous, blockId),
			})
			return
		}
		definedIn[id] = blockId
		definedAt[id] = position
	}

	entry := graph.Entry(function)
	for _, param := range graph.Params(function) {
		recordDefinition(graph.IdentifierOf(param), entry, -1)
	}

	// The visitors are made once and read the block and position being walked from these: one handed
	// to a Graph method escapes, so one per instruction would be an allocation per instruction.
	var blockId BlockId
	var currentBlock B
	position, count := 0, 0
	isContextStore := false
	recordInstruction := func(place *P, role Role) {
		if role != Define {
			return
		}
		if isContextStore && !graph.ContextStoreDefines(function, currentBlock, position, *place) {
			return
		}
		recordDefinition(graph.IdentifierOf(*place), blockId, position)
	}
	recordTerminal := func(place *P, role Role) {
		if role == Define {
			recordDefinition(graph.IdentifierOf(*place), blockId, count)
		}
	}

	for _, block := range blocks {
		blockId, currentBlock = graph.Id(block), block
		// A phi's result is defined at the top of its block, before every instruction.
		for _, phi := range graph.Phis(block) {
			recordDefinition(graph.IdentifierOf(phi.Place), blockId, -1)
		}
		count = graph.InstructionCount(function, block)
		for position = 0; position < count; position++ {
			// # A context binding is written many times, and upstream's invariant never sees it
			//
			// `AssertConsistentIdentifiers.ts:43` asserts "Expected lvalues to be assigned exactly
			// once" against `instr.lvalue.identifier` -- the instruction's own temporary. A
			// `StoreContext` writes its binding through a different field, `value.lvalue.place`,
			// which is never added to that set. So upstream's context binding, written at its
			// declaration and again at every reassignment, does not trip its own check.
			//
			// Measured on the pinned build: both writes name `lvalueId=2`, one `kind=Let` and one
			// `kind=Reassign`, and the invariant passes.
			//
			// This walk records every definition, which includes a store's inner lvalue, so the same
			// binding reads as defined twice. Narrowed to match upstream's subject rather than widened
			// generally: an ordinary store still reports, and only the place a context store writes
			// through is exempt.
			isContextStore = graph.IsContextStore(function, block, position)
			graph.EachInstructionPlace(function, block, position, recordInstruction)
		}
		graph.EachTerminalPlace(block, recordTerminal)
	}

	// checkUse verifies one use sitting in useBlock at usePosition.
	checkUse := func(id IdentifierId, useBlock BlockId, usePosition int, what string) {
		definitionBlock, defined := definedIn[id]
		if !defined {
			// Never defined in this function: a global, an import, or a capture. Not a violation;
			// SSA says nothing about values this function does not define.
			return
		}
		if definitionBlock == useBlock {
			if definedAt[id] > usePosition {
				violations = append(violations, SSAViolation{
					Kind:       SSAViolationUseNotDominated,
					Identifier: id,
					Block:      useBlock,
					Detail: fmt.Sprintf("%s in bb%d reads %s before it is defined in the same block",
						what, useBlock, graph.PlaceString(function, id)),
				})
			}
			return
		}
		if !dominance.Dominates(definitionBlock, useBlock) {
			violations = append(violations, SSAViolation{
				Kind:       SSAViolationUseNotDominated,
				Identifier: id,
				Block:      useBlock,
				Detail: fmt.Sprintf("%s in bb%d reads %s defined in bb%d, which does not dominate bb%d",
					what, useBlock, graph.PlaceString(function, id), definitionBlock, useBlock),
			})
		}
	}

	checkInstruction := func(place *P, role Role) {
		if role != Define {
			checkUse(graph.IdentifierOf(*place), blockId, position, "instruction")
		}
	}
	checkTerminal := func(place *P, role Role) {
		if role != Define {
			checkUse(graph.IdentifierOf(*place), blockId, count, "terminal")
		}
	}

	for _, block := range blocks {
		blockId = graph.Id(block)
		// A phi operand is read on the edge from its predecessor, so it must be dominated by the
		// PREDECESSOR's exit rather than by the phi's own block. See the file comment.
		for _, phi := range graph.Phis(block) {
			for _, predecessorId := range PhiOperandsInOrder(phi) {
				operand := phi.Operands.At(predecessorId)
				predecessor, ok := graph.Block(function, predecessorId)
				if !ok {
					continue
				}
				checkUse(graph.IdentifierOf(operand), predecessorId, graph.InstructionCount(function, predecessor),
					fmt.Sprintf("phi operand for bb%d", predecessorId))
			}
		}
		count = graph.InstructionCount(function, block)
		for position = 0; position < count; position++ {
			graph.EachInstructionPlace(function, block, position, checkInstruction)
		}
		graph.EachTerminalPlace(block, checkTerminal)
	}

	return violations
}

// SSAStats counts what a verification actually had to look at.
//
// This exists because an empty violation list is exactly what a vacuous check returns. A function
// with no phis and no renamed values passes `VerifySSA` perfectly while proving nothing, and that
// is the failure mode this whole package has been bitten by twice. A caller reporting a clean run
// should report these numbers alongside it.
type SSAStats struct {
	// Phis is how many merge points were placed.
	Phis int
	// NamedValues is how many defined values carry a source name rather than being a temporary.
	// Zero here means no source variable was ever resolved, so nothing was renamed.
	NamedValues int
	// Uses is how many use sites were checked for dominance.
	Uses int
}

// CollectSSAStats measures one function.
func CollectSSAStats[G Graph[F, B, P], F any, B comparable, P any](graph G, function F) SSAStats {
	var stats SSAStats
	seen := map[IdentifierId]bool{}
	note := func(place P) {
		id := graph.IdentifierOf(place)
		if seen[id] {
			return
		}
		seen[id] = true
		if graph.Named(function, id) {
			stats.NamedValues++
		}
	}
	instruction := func(place *P, role Role) {
		if role == Define {
			note(*place)
			return
		}
		stats.Uses++
	}
	terminal := func(place *P, role Role) {
		if role != Define {
			stats.Uses++
		}
	}
	for _, block := range graph.Blocks(function) {
		phis := graph.Phis(block)
		stats.Phis += len(phis)
		for _, phi := range phis {
			note(phi.Place)
		}
		count := graph.InstructionCount(function, block)
		for index := 0; index < count; index++ {
			graph.EachInstructionPlace(function, block, index, instruction)
		}
		graph.EachTerminalPlace(block, terminal)
	}
	return stats
}

// Dominance is the immediate-dominator array over a function's block slice, by position in it.
type Dominance struct {
	// position maps a block id to its index in the order ComputeDominance made: a reverse postorder of
	// the real edges from the entry, then the blocks they don't reach.
	position map[BlockId]int
	// immediate[i] is the index of block i's immediate dominator; the entry is its own, and a block the
	// real edges don't reach has none (-1), dominated by nothing but itself.
	immediate []int
}

// ComputeDominance runs Cooper-Harvey-Kennedy over the function's blocks.
//
// # Why it orders the blocks itself rather than reading the block slice
//
// The algorithm needs every block after some predecessor of it, so that each immediate dominator comes
// before its block and intersect's walk up the tree ends where it should. A reverse postorder of the
// real edges guarantees that. The block slice is not always one: ReversePostorder visits a structural
// fallthrough first, as upstream does, and keeps a block where the fallthrough first reached it, so a
// loop that a fallthrough reaches before its back edge sits ahead of every real predecessor it has. Over
// the slice, the fixed point then settled on too few dominators: 8 of 2,000 generated graphs disagreed
// with dominance computed as plain set intersection, among them a block every path reaches through
// three others that was dominated, the answer said, by the entry and one of them (#6v4a54x). The slice's
// order is what evaluation order and every other pass rely on, so it stays, and this pass walks the
// real edges itself.
//
// Blocks the real edges don't reach (a fallthrough's placeholder) follow the reverse postorder, in the
// slice's order, with no dominator but themselves.
func ComputeDominance[G Graph[F, B, P], F any, B comparable, P any](graph G, function F) *Dominance {
	blocks, reached := realReversePostorder(graph, function)
	tree := &Dominance{position: make(map[BlockId]int, len(blocks))}
	for index, block := range blocks {
		tree.position[graph.Id(block)] = index
	}
	tree.immediate = make([]int, len(blocks))
	for index := range tree.immediate {
		tree.immediate[index] = -1
	}
	if reached == 0 {
		return tree
	}
	tree.immediate[0] = 0
	blocks = blocks[:reached]

	intersect := func(a, b int) int {
		for a != b {
			for a > b {
				a = tree.immediate[a]
			}
			for b > a {
				b = tree.immediate[b]
			}
		}
		return a
	}

	for changed := true; changed; {
		changed = false
		for index := 1; index < len(blocks); index++ {
			newImmediate := -1
			for _, predecessorId := range graph.Predecessors(blocks[index]) {
				predecessorIndex, ok := tree.position[predecessorId]
				if !ok || tree.immediate[predecessorIndex] == -1 {
					continue
				}
				if newImmediate == -1 {
					newImmediate = predecessorIndex
				} else {
					newImmediate = intersect(predecessorIndex, newImmediate)
				}
			}
			if newImmediate != -1 && tree.immediate[index] != newImmediate {
				tree.immediate[index] = newImmediate
				changed = true
			}
		}
	}
	return tree
}

// realReversePostorder is the function's blocks in a reverse postorder of the real edges from its entry,
// then the blocks those edges don't reach, in the block slice's order; and how many the edges reach.
// Fallthroughs are not edges. Exceptional edges are real ones, as everywhere in this module.
func realReversePostorder[G Graph[F, B, P], F any, B comparable, P any](graph G, function F) ([]B, int) {
	slice := graph.Blocks(function)
	visited := make(map[BlockId]bool, len(slice))
	postorder := make([]B, 0, len(slice))
	type frame struct {
		block      B
		successors []BlockId
		next       int
	}
	var stack []frame
	scratch := edgeScratchPool.Get().(*edgeScratch)
	defer edgeScratchPool.Put(scratch)
	enter := func(id BlockId) {
		block, ok := graph.Block(function, id)
		if !ok || visited[id] {
			return
		}
		visited[id] = true
		var successors []BlockId
		for _, edge := range edgesOf(graph, block, scratch) {
			if edge.edge != Fallthrough {
				successors = append(successors, edge.successor)
			}
		}
		stack = append(stack, frame{block: block, successors: successors})
	}

	enter(graph.Entry(function))
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next == len(top.successors) {
			postorder = append(postorder, top.block)
			stack = stack[:len(stack)-1]
			continue
		}
		next := top.successors[top.next]
		top.next++
		enter(next)
	}

	order := make([]B, 0, len(slice))
	for index := len(postorder) - 1; index >= 0; index-- {
		order = append(order, postorder[index])
	}
	for _, block := range slice {
		if !visited[graph.Id(block)] {
			order = append(order, block)
		}
	}
	return order, len(postorder)
}

// entryPredecessors reports the entry block and its predecessors when it has any, which Graph.Entry
// rules out: Construct's lookup walks back through predecessors until a block has none, and an entry
// some edge enters can put it on a cycle that never ends.
func entryPredecessors[G Graph[F, B, P], F any, B comparable, P any](graph G, function F) (BlockId, []BlockId, bool) {
	entry := graph.Entry(function)
	block, ok := graph.Block(function, entry)
	if !ok {
		return entry, nil, false
	}
	predecessors := graph.Predecessors(block)
	return entry, predecessors, len(predecessors) > 0
}

// Dominates reports whether every path from the entry to block passes through dominator.
//
// A block dominates itself, the standard convention.
func (t *Dominance) Dominates(dominator, block BlockId) bool {
	target, ok := t.position[dominator]
	if !ok {
		return false
	}
	index, ok := t.position[block]
	if !ok {
		return false
	}
	for {
		if index == target {
			return true
		}
		if index == 0 || t.immediate[index] == -1 {
			return false
		}
		index = t.immediate[index]
	}
}
