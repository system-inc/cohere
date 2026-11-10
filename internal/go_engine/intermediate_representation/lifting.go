// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// Lifting (#ee33njz): go/ir's lift.go, its loop `for lift(f) {}` and everything a round does, with the shared
// core's construction (Braun's, in static_single_assignment) in place of lift.go's dominance frontiers and
// renaming walk. A round:
//
//   - finds the liftable Allocations (lifting_liftable.go, go/ir's liftable), splitting one whose address escapes
//     partway through;
//   - lifts them through the core (lifting_graph.go is the adapter), then resolves each value the core named: a
//     Load becomes the value that reaches it, a core phi a *Phi at its block's head with Edges in Predecessors
//     order, and the lifted Allocation, Store and Load go;
//   - simplifies the new phis as go/ir's simplifyPhis does, and removes the ones nothing but a phi reads, as its
//     markLiveNodes does;
//   - removes the ssa:deferstack call when the function defers nothing that needs it, and RunDefers when it
//     defers nothing at all, as lift does while it is there.
//
// Lifting one Allocation can make another liftable (a Load through a lifted pointer becomes a Load from the
// pointee's own cell), so rounds repeat until one lifts nothing, as go/ir's do.
//
// go/ir follows lifting with simplifyConstantCompositeValues only when doSimplifyConstantCompositeValues is set,
// and it is not ("it breaks field tracking in U1000"), so buildir never runs it and neither does ours.

import (
	"fmt"
	"math"
	"slices"
	"sync"

	"github.com/system-inc/cohere/static_single_assignment"
)

// liftingOptions are how a test asks a Program's functions to be built. The zero value lifts, as the engine does.
type liftingOptions struct {
	// unlifted builds every function as go/ir's NaiveForm does, for the differential against it.
	unlifted bool

	// verify, when set, is handed the core's verdict on each round of each function, right after construction,
	// while the values still carry the core's names, and what liftingPlacedProblems finds of the core's view: where
	// it is incomplete, and how many instructions have a place.
	verify func(function *Function, violations []static_single_assignment.StaticSingleAssignmentViolation,
		incomplete []string, places int)

	// placedDropsStores plants a mutant: each block's placed view leaves out its Stores.
	placedDropsStores bool

	// skipStoreDefinition plants a mutant: the first Store a round would lift defines nothing.
	skipStoreDefinition bool

	// dropDebugReference plants a mutant: each function's first debug reference goes unrecorded in its
	// expression map.
	dropDebugReference bool
}

// liftingOptionsByProgram holds the options a test set on a Program, *Program to liftingOptions. A Program
// nothing set lifts.
var liftingOptionsByProgram sync.Map

func liftingOptionsOf(program *Program) liftingOptions {
	stored, isStored := liftingOptionsByProgram.Load(program)
	if !isStored {
		return liftingOptions{}
	}
	options, isOptions := stored.(liftingOptions)
	if !isOptions {
		return liftingOptions{}
	}
	return options
}

// liftFunction lifts function's liftable Allocations, round after round until one lifts nothing. finishBody calls
// it once the blocks, referrers and dominator tree are built.
func liftFunction(function *Function) {
	options := liftingOptionsOf(function.Program)
	if options.unlifted {
		return
	}
	scratch, isScratch := liftingScratchPool.Get().(*liftingScratch)
	if !isScratch {
		// The pool's New makes every value it holds, so this is only a pool emptied by a collection.
		scratch = &liftingScratch{}
	}
	for liftingRound(function, options, scratch) {
	}
	scratch.release()
	liftingScratchPool.Put(scratch)
}

// liftingRound is one of go/ir's lift calls, and reports whether it lifted anything.
//
// Mutates: function lifting rewrites its blocks and Locals in place, as go/ir's lift does
// Mutates: scratch the round's tables are kept in it, for the next round to reuse
func liftingRound(function *Function, options liftingOptions, scratch *liftingScratch) bool {
	usesDefer := false
	deferStackAllocation, deferStackCall := liftingDeferStackPreamble(function)
	eliminateDeferStack := deferStackAllocation != nil && !deferStackAllocation.Heap

	scratch.work = liftingCleared(scratch.work, len(function.Blocks))
	liftingNumber(function, scratch)
	scratch.runDefers = liftingCleared(scratch.runDefers, len(function.Blocks))
	scratch.allocations = scratch.allocations[:0]
	// Cleared whether or not the function has Locals, so a lookup never finds an earlier round's.
	clear(scratch.localsInBlocks)
	if len(function.Locals) > 0 {
		if scratch.localsInBlocks == nil {
			scratch.localsInBlocks = make(map[*Allocation]bool, len(function.Locals))
		}
		scratch.localsHeld = max(scratch.localsHeld, len(function.Locals))
		for _, local := range function.Locals {
			scratch.localsInBlocks[local] = false
		}
	}
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			switch instruction := instruction.(type) {
			case *Allocation:
				if _, isLocal := scratch.localsInBlocks[instruction]; isLocal {
					scratch.localsInBlocks[instruction] = true
				}
				if liftingLiftable(instruction, scratch.work, scratch.heads, &scratch.liftable) {
					scratch.allocations = append(scratch.allocations, instruction)
				}
			case *Defer:
				usesDefer = true
				if eliminateDeferStack && instruction.DeferStack != nil {
					// Clear the deferred call's stack and the reference to it.
					if referrers := instruction.DeferStack.Referrers(); referrers != nil {
						*referrers = liftingRemoveReferrer(*referrers, instruction)
					}
					instruction.DeferStack = nil
				}
			case *RunDefers:
				scratch.runDefers[block.Index]++
			}
		}
	}

	var lifting *liftingFunction
	if len(scratch.allocations) > 0 {
		liftingApplyWork(function, scratch.work)
		lifting = liftingPrepare(function, scratch.allocations, options, scratch)
		static_single_assignment.Construct(liftingGraph{}, lifting)
		if options.verify != nil {
			incomplete, places := liftingPlacedProblems(lifting)
			options.verify(function, static_single_assignment.VerifyStaticSingleAssignment(liftingGraph{}, lifting),
				incomplete, places)
		}
		liftingResolve(lifting, scratch)
		// A lifted Allocation leaves Locals with the ones no block holds.
		for _, allocation := range scratch.allocations {
			if _, isLocal := scratch.localsInBlocks[allocation]; isLocal {
				scratch.localsInBlocks[allocation] = false
			}
		}
	}
	// The ssa:deferstack call goes once lifting has removed what read it.
	var deletedCall Instruction
	if lifting != nil && eliminateDeferStack {
		deletedCall = deferStackCall
	}

	// Prepend the live phis to each block, drop what lifting deleted, and RunDefers where nothing defers.
	for _, block := range function.Blocks {
		var lifted *liftingBlock
		if lifting != nil {
			lifted = lifting.blocks[block.Index]
		}
		var head []*Phi
		var slots []liftingSlot
		if lifted != nil {
			head, slots = lifted.newPhis, lifted.slots
		}
		holdsCall := deletedCall != nil && deletedCall.Block() == block
		if len(head) == 0 && slots == nil && !holdsCall && (usesDefer || scratch.runDefers[block.Index] == 0) {
			continue
		}
		liftingRewriteBlock(block, head, func(index int, instruction Instruction) bool {
			if instruction == nil || instruction == deletedCall || (index < len(slots) && slots[index].deleted) {
				return false
			}
			_, isRunDefers := instruction.(*RunDefers)
			return !isRunDefers || usesDefer
		})
	}

	// Remove the lifted Allocations from Locals, and those no block holds any longer, the block optimization
	// having removed their blocks as unreachable. go/ir drops the second kind by accident, and ours as go/ir does:
	// its lift keeps a local whose index the round set to -1, the mark of an unliftable Allocation, and an
	// Allocation in no block keeps its zero index, the mark of a lifted one.
	function.Locals = slices.DeleteFunc(function.Locals, func(local *Allocation) bool {
		return !scratch.localsInBlocks[local]
	})
	return len(scratch.allocations) > 0
}

// liftingRewriteBlock makes block's instructions head, then each of its instructions keep accepts, in order, keep
// handed each one's index in the block as it was. It rewrites them in the block's own array when that holds both,
// and makes a new one only when the phis outnumber what goes and the array's spare capacity (#npb86tz): a round
// rewrites every block it lifted in, and a new array for each was the largest thing a round made.
//
// Mutates: block its instructions are rewritten, in its own array where they fit
func liftingRewriteBlock(block *BasicBlock, head []*Phi, keep func(index int, instruction Instruction) bool) {
	// Compacting writes at an index no later than the one it reads, so it never overwrites one still to read.
	kept := 0
	for index, instruction := range block.Instructions {
		if keep(index, instruction) {
			block.Instructions[kept] = instruction
			kept++
		}
	}
	length := len(head) + kept
	if length > cap(block.Instructions) {
		instructions := make([]Instruction, length)
		copy(instructions[len(head):], block.Instructions[:kept])
		for index, phi := range head {
			instructions[index] = phi
		}
		block.Instructions = instructions
		return
	}
	// The kept instructions move past the phis, copy handling the overlap, and the tail the round dropped is cleared
	// so the array holds nothing it no longer lists.
	spanned := block.Instructions[:max(length, len(block.Instructions))]
	copy(spanned[len(head):length], spanned[:kept])
	for index, phi := range head {
		spanned[index] = phi
	}
	clear(spanned[length:])
	block.Instructions = spanned[:length]
}

// liftingNumber numbers each block's instructions from zero, as liftable reads them, and sets scratch's heads to
// each block's first non-phi index, or -1 for a block of phis alone.
//
// Mutates: scratch its heads are the round's, in the array the last round's were
func liftingNumber(function *Function, scratch *liftingScratch) {
	scratch.heads = liftingCleared(scratch.heads, len(function.Blocks))
	for _, block := range function.Blocks {
		scratch.heads[block.Index] = -1
		var number Id
		for index, instruction := range block.Instructions {
			if instruction == nil {
				continue
			}
			instruction.setId(number)
			number++
			if _, isPhi := instruction.(*Phi); !isPhi && scratch.heads[block.Index] == -1 {
				scratch.heads[block.Index] = index
			}
		}
	}
}

// liftingApplyWork makes the changes liftable recorded: each rename, then each insertion.
func liftingApplyWork(function *Function, work []liftingWork) {
	for _, block := range function.Blocks {
		for _, rename := range work[block.Index].renameAllocations {
			for _, instruction := range block.Instructions[rename.startingAt:] {
				liftingReplace(instruction, rename.from, rename.to)
			}
		}
	}
	for _, block := range function.Blocks {
		insertions := work[block.Index].insertInstructions
		if len(insertions) == 0 {
			continue
		}
		instructions := make([]Instruction, 0, len(block.Instructions)+len(insertions)*3)
		for _, instruction := range block.Instructions {
			instructions = append(instructions, insertions[instruction]...)
			instructions = append(instructions, instruction)
		}
		block.Instructions = instructions
	}
}

// liftingPrepare is function as the core sees it, with a place on each lifted Allocation, each Store to one and
// each Load from one, made in scratch's lifting from scratch's arrays.
//
// Mutates: scratch its lifting is the round's, and its arrays hold the round's blocks and slots
func liftingPrepare(function *Function, allocations []*Allocation, options liftingOptions,
	scratch *liftingScratch) *liftingFunction {
	lifting := &scratch.lifting
	lifting.function = function
	lifting.allocations = allocations
	lifting.declarations = liftingCleared(lifting.declarations, len(allocations))
	lifting.blocks = liftingCleared(lifting.blocks, len(function.Blocks))
	scratch.blockValues = liftingCleared(scratch.blockValues, len(function.Blocks))
	instructions := 0
	for _, block := range function.Blocks {
		instructions += len(block.Instructions)
	}
	scratch.slots = liftingCleared(scratch.slots, instructions)
	// Each instruction's id is its index in its block for the round, so a lifted cell, Load or debug reference is
	// found by its block and id rather than through a map (#beapx0q). finishBody numbers them again after lifting.
	start := 0
	for blockIndex, block := range function.Blocks {
		end := start + len(block.Instructions)
		scratch.blockValues[blockIndex] = liftingBlock{block: block, vacantSlots: scratch.slots[start:end:end]}
		lifting.blocks[blockIndex] = &scratch.blockValues[blockIndex]
		start = end
		for index, instruction := range block.Instructions {
			instruction.setId(Id(index))
		}
	}
	for index, allocation := range allocations {
		lifting.declarations[index] = static_single_assignment.DeclarationId(index)
		block := lifting.blocks[allocation.block.Index]
		liftingPlaceAt(block, int(allocation.id), static_single_assignment.DeclarationId(index),
			static_single_assignment.Define)
		block.slots[allocation.id].isCell = true
	}
	storeSkipped := !options.skipStoreDefinition
	for _, lifted := range lifting.blocks {
		for index, instruction := range lifted.block.Instructions {
			switch instruction := instruction.(type) {
			case *Store:
				cell := liftingCellSlot(lifting, instruction.Address)
				if cell == nil {
					continue
				}
				if !storeSkipped {
					storeSkipped = true
					continue
				}
				liftingPlaceAt(lifted, index, liftingDeclarationOf(cell), static_single_assignment.Define)
			case *Load:
				if cell := liftingCellSlot(lifting, instruction.X); cell != nil {
					liftingPlaceAt(lifted, index, liftingDeclarationOf(cell), static_single_assignment.Use)
				}
			case *debugReference:
				// A reference to the variable's address reads the value there once lifted, as a Load does; one
				// to the cell's own address names what lifting removes, and goes, as go/ir's renaming has it.
				cell := liftingCellSlot(lifting, instruction.X)
				switch {
				case cell == nil:
				case instruction.IsAddress:
					liftingPlaceAt(lifted, index, liftingDeclarationOf(cell), static_single_assignment.Use)
				default:
					instruction.X = nil
					liftingDeleteAt(lifted, index)
				}
			}
		}
	}
	liftingCarvePlaced(lifting, options, scratch)
	lifting.order = liftingReversePostorder(lifting, scratch)
	return lifting
}

// liftingCarvePlaced sets each block's placed view, every view carved from one backing array for the round, sized
// by counting the places first: one array a round, where a slice grown per block allocates and copies as it goes
// (#beapx0q). The array is scratch's placed, reused when it holds them all (#npb86tz).
//
// An index is int32, which holds any index a block can have: a block's instructions are a slice of interfaces, 16
// bytes each, so a block past math.MaxInt32 of them would hold 32 GiB in that slice alone. The check makes the
// bound loud should it ever be crossed, rather than wrapping.
//
// Mutates: lifting its blocks' placed views are set
// Mutates: scratch its placed array holds the round's views
func liftingCarvePlaced(lifting *liftingFunction, options liftingOptions, scratch *liftingScratch) {
	inView := func(block *liftingBlock, index int) bool {
		if !block.slots[index].isPlace {
			return false
		}
		_, isStore := block.block.Instructions[index].(*Store)
		return !isStore || !options.placedDropsStores
	}
	places := 0
	for _, block := range lifting.blocks {
		if len(block.slots) > math.MaxInt32 {
			// Invariant: a block past math.MaxInt32 instructions would hold 32 GiB in its slice of them alone.
			panic(fmt.Sprintf("lifting %s: block %d holds %d instructions, past an int32 index",
				lifting.function.RelativeString(nil), block.block.Index, len(block.slots)))
		}
		for index := range block.slots {
			if inView(block, index) {
				places++
			}
		}
	}
	// Sized before any view is carved, so no append below moves the array a view is carved from.
	scratch.placed = liftingCleared(scratch.placed, places)[:0]
	for _, block := range lifting.blocks {
		start := len(scratch.placed)
		for index := range block.slots {
			if inView(block, index) {
				scratch.placed = append(scratch.placed, int32(index))
			}
		}
		block.placed = scratch.placed[start:len(scratch.placed):len(scratch.placed)]
	}
}

// liftingCellSlot is the slot of the lifted Allocation value is, or nil when value is no lifted Allocation of the
// function.
func liftingCellSlot(lifting *liftingFunction, value Value) *liftingSlot {
	allocation, isAllocation := value.(*Allocation)
	if !isAllocation || allocation.block == nil {
		return nil
	}
	return liftingSlotOf(lifting, allocation, allocation.block, func(slot liftingSlot) bool { return slot.isCell })
}

// liftingSlotOf is the slot of instruction, which sits in block, when keep accepts it, or nil.
func liftingSlotOf(lifting *liftingFunction, instruction Instruction, block *BasicBlock,
	keep func(slot liftingSlot) bool) *liftingSlot {
	if block.Index >= len(lifting.blocks) || lifting.blocks[block.Index].block != block {
		return nil
	}
	lifted := lifting.blocks[block.Index]
	position := int(instruction.Id())
	if position >= len(lifted.slots) || lifted.block.Instructions[position] != instruction ||
		!keep(lifted.slots[position]) {
		return nil
	}
	return &lifted.slots[position]
}

// liftingDeclarationOf is the declaration a lifted cell's slot defines. Before construction a declaration's own
// identifier is its number.
func liftingDeclarationOf(cell *liftingSlot) static_single_assignment.DeclarationId {
	return static_single_assignment.DeclarationId(cell.place.identifier)
}

// liftingPlaceAt gives block's instruction at index a place of declaration in role, taking the block's slots from
// its vacant ones on its first.
//
// Mutates: block its slots are where the core finds each instruction's place
func liftingPlaceAt(block *liftingBlock, index int, declaration static_single_assignment.DeclarationId,
	role static_single_assignment.Role) {
	if block.slots == nil {
		block.slots = block.vacantSlots
	}
	// A declaration's own identifier is its number, so the first identifiers are the declarations'.
	identifier := static_single_assignment.IdentifierId(declaration)
	block.slots[index] = liftingSlot{place: liftingPlace{identifier: identifier}, role: role, isPlace: true}
}

// liftingReversePostorder is the blocks in reverse postorder from the entry over Successors, then the blocks the
// entry doesn't reach, in index order: Braun's construction needs every predecessor but a back edge's walked
// before a block. It is scratch's order, walked with scratch's visited, each in the array the last round's was.
//
// Mutates: scratch its visited and order are the round's
func liftingReversePostorder(lifting *liftingFunction, scratch *liftingScratch) []*liftingBlock {
	scratch.visited = liftingCleared(scratch.visited, len(lifting.blocks))
	scratch.order = scratch.order[:0]
	var visit func(block *BasicBlock)
	visit = func(block *BasicBlock) {
		scratch.visited[block.Index] = true
		for _, successor := range block.Successors {
			if !scratch.visited[successor.Index] {
				visit(successor)
			}
		}
		scratch.order = append(scratch.order, lifting.blocks[block.Index])
	}
	visit(lifting.function.Blocks[0])
	slices.Reverse(scratch.order)
	for _, block := range lifting.blocks {
		if !scratch.visited[block.block.Index] {
			scratch.order = append(scratch.order, block)
		}
	}
	return scratch.order
}

// liftingDefinition is what the core's construction made one identifier: the zero of an Allocation, the operand
// of a Store, or a phi, at most one of them; none for a declaration's own identifier. value is the identifier's
// value once resolved.
type liftingDefinition struct {
	zero  *Allocation
	store *Store
	phi   *Phi
	value Value
}

// liftingResolve turns the core's names into values, marks on the slots what lifting deletes, and leaves on each
// block the new phis to prepend.
//
// Its tables are dense, by identifier, sized to every identifier the core minted: Mint appends to
// lifting.declarations, so its length bounds them all (#beapx0q). The definitions are scratch's, reused (#npb86tz).
//
// Mutates: lifting its blocks' slots are marked deleted, and their newPhis set
// Mutates: scratch its definitions are the round's
func liftingResolve(lifting *liftingFunction, scratch *liftingScratch) {
	scratch.definitions = liftingCleared(scratch.definitions, len(lifting.declarations))
	for _, block := range lifting.blocks {
		for index, slot := range block.slots {
			if !slot.isPlace {
				continue
			}
			switch instruction := block.block.Instructions[index].(type) {
			case *Allocation:
				scratch.definitions[slot.place.identifier].zero = instruction
			case *Store:
				scratch.definitions[slot.place.identifier].store = instruction
			}
		}
	}

	// Each core phi becomes a *Phi first, so a value can name one before its edges are filled.
	type placedPhi struct {
		block *BasicBlock
		core  *static_single_assignment.Phi[liftingPlace]
		phi   *Phi
	}
	var placed []placedPhi
	for _, block := range lifting.order {
		for _, core := range block.phis {
			allocation := lifting.allocations[lifting.declarations[core.Place.identifier]]
			phi := &Phi{Edges: make([]Value, len(block.block.Predecessors))}
			phi.comment = allocation.comment
			phi.setSource(allocation.source)
			phi.setType(dereference(allocation.Type()))
			phi.setBlock(block.block)
			scratch.definitions[core.Place.identifier].phi = phi
			placed = append(placed, placedPhi{block: block.block, core: core, phi: phi})
		}
	}

	var valueOf func(identifier static_single_assignment.IdentifierId) Value
	through := func(value Value) Value {
		if load, isLoad := value.(*Load); isLoad && load.block != nil {
			slot := liftingSlotOf(lifting, load, load.block, func(slot liftingSlot) bool { return slot.isPlace })
			if slot != nil {
				return valueOf(slot.place.identifier)
			}
		}
		return value
	}
	valueOf = func(identifier static_single_assignment.IdentifierId) Value {
		definition := &scratch.definitions[identifier]
		if definition.value != nil {
			return definition.value
		}
		switch {
		case definition.phi != nil:
			definition.value = definition.phi
		case definition.store != nil:
			definition.value = through(definition.store.Value)
		default:
			// An Allocation's own zero, or, should a Load ever see no definition, the zero go/ir's renaming
			// gives a cell nothing has stored to.
			allocation := definition.zero
			if allocation == nil {
				allocation = lifting.allocations[lifting.declarations[identifier]]
			}
			scratch.definitions[identifier].value = zeroConstant(dereference(allocation.Type()), allocation.source)
		}
		return scratch.definitions[identifier].value
	}

	created := make([]*Phi, 0, len(placed))
	for _, each := range placed {
		for index, predecessor := range each.block.Predecessors {
			operand, _ := each.core.Operands.Get(static_single_assignment.BlockId(predecessor.Index))
			edge := valueOf(operand.identifier)
			each.phi.Edges[index] = edge
			if referrers := edge.Referrers(); referrers != nil {
				*referrers = append(*referrers, each.phi)
			}
		}
		created = append(created, each.phi)
	}

	// Every use of a lifted Load reads the value that reaches it, then the lifted Store and Allocation go.
	for _, block := range lifting.blocks {
		for index, slot := range block.slots {
			if !slot.isPlace {
				continue
			}
			if load, isLoad := block.block.Instructions[index].(*Load); isLoad {
				liftingReplaceAll(load, valueOf(slot.place.identifier))
				block.slots[index].deleted = true
			}
		}
	}
	// A debug reference to a lifted variable's address now names the value there, as go/ir's renaming makes it;
	// one to the address of a lifted cell names what no longer exists, and goes.
	// (The prepare dropped those to a cell's own address.)
	for _, block := range lifting.blocks {
		for index, slot := range block.slots {
			if !slot.isPlace {
				continue
			}
			reference, isReference := block.block.Instructions[index].(*debugReference)
			if !isReference {
				continue
			}
			reference.X = valueOf(slot.place.identifier)
			reference.IsAddress = false
			if referrers := reference.X.Referrers(); referrers != nil {
				*referrers = append(*referrers, reference)
			}
		}
	}
	for _, block := range lifting.blocks {
		for index, slot := range block.slots {
			if !slot.isPlace {
				continue
			}
			switch instruction := block.block.Instructions[index].(type) {
			case *Store:
				if referrers := instruction.Address.Referrers(); referrers != nil {
					*referrers = liftingRemoveReferrer(*referrers, instruction)
				}
				if referrers := instruction.Value.Referrers(); referrers != nil {
					*referrers = liftingRemoveReferrer(*referrers, instruction)
				}
				block.slots[index].deleted = true
			case *Allocation:
				block.slots[index].deleted = true
			}
		}
	}

	for _, phi := range liftingLivePhis(lifting.function, created) {
		block := lifting.blocks[phi.block.Index]
		block.newPhis = append(block.newPhis, phi)
	}
}

// liftingDeleteAt marks block's instruction at index for the round to drop, taking the block's slots from its vacant
// ones on its first.
//
// Mutates: block its slots carry what the round drops
func liftingDeleteAt(block *liftingBlock, index int) {
	if block.slots == nil {
		block.slots = block.vacantSlots
	}
	block.slots[index].deleted = true
}

// liftingLivePhis simplifies the new phis as go/ir's simplifyPhis does, then keeps the ones go/ir's
// markLiveNodes keeps: a phi something other than a phi reads, and every phi such a phi or a phi the builder
// made reads. The rest are dropped and leave their edges' referrers.
//
// go/ir leaves a phi simplifyPhis replaced in its block, with no referrers, and ours drops it: Braun's construction
// with the core's elimination never makes most of those phis, so keeping the rest would match nothing.
func liftingLivePhis(function *Function, created []*Phi) []*Phi {
	replaced := map[*Phi]bool{}
	for changed := true; changed; {
		changed = false
		for _, phi := range created {
			if replaced[phi] {
				continue
			}
			if value, useless := liftingUselessPhi(phi); useless {
				liftingReplaceAll(phi, value)
				replaced[phi] = true
				changed = true
			}
		}
	}

	live := map[*Phi]bool{}
	var markLive func(phi *Phi)
	markLive = func(phi *Phi) {
		live[phi] = true
		for _, edge := range phi.Edges {
			if edgePhi, isPhi := edge.(*Phi); isPhi && !live[edgePhi] {
				markLive(edgePhi)
			}
		}
	}
	for _, phi := range created {
		if !replaced[phi] && !live[phi] && liftingHasDirectReferrer(phi) {
			markLive(phi)
		}
	}
	// The phis the builder made, for && and || and per-iteration loop variables, are all live (Go issue 19622).
	for _, block := range function.Blocks {
		for _, instruction := range block.phis() {
			if phi, isPhi := instruction.(*Phi); isPhi {
				markLive(phi)
			}
		}
	}

	var kept []*Phi
	for _, phi := range created {
		if live[phi] && !replaced[phi] {
			kept = append(kept, phi)
			continue
		}
		for _, edge := range phi.Edges {
			if referrers := edge.Referrers(); referrers != nil {
				*referrers = liftingRemoveReferrer(*referrers, phi)
			}
		}
	}
	return kept
}

// liftingUselessPhi is go/ir's isUselessPhi: the one value a phi's edges all name but for the phi itself, where
// two constants of one type and value count as one.
func liftingUselessPhi(phi *Phi) (Value, bool) {
	var first Value
	for _, edge := range phi.Edges {
		if edge == phi {
			continue
		}
		if first == nil {
			first = edge
			continue
		}
		if first == edge {
			continue
		}
		firstConstant, firstIsConstant := first.(*Constant)
		edgeConstant, edgeIsConstant := edge.(*Constant)
		if firstIsConstant && edgeIsConstant && firstConstant.valueType == edgeConstant.valueType &&
			firstConstant.Value == edgeConstant.Value {
			continue
		}
		return nil, false
	}
	return first, first != nil
}

// liftingHasDirectReferrer reports whether something other than a phi reads value.
func liftingHasDirectReferrer(value Value) bool {
	for _, referrer := range *value.Referrers() {
		if _, isPhi := referrer.(*Phi); !isPhi {
			return true
		}
	}
	return false
}

// liftingDeferStackPreamble is the Allocation holding the function's ssa:deferstack() and the call that fills it,
// or nils when the function has none.
func liftingDeferStackPreamble(function *Function) (*Allocation, *Call) {
	allocation, _ := function.vars[function.deferstack].(*Allocation)
	if allocation == nil {
		return nil, nil
	}
	for _, referrer := range *allocation.Referrers() {
		if store, isStore := referrer.(*Store); isStore && store.Address == allocation {
			if call, isCall := store.Value.(*Call); isCall {
				return allocation, call
			}
		}
	}
	return nil, nil
}

// liftingReplaceAll moves every use of from to to, as go/ir's replaceAll does, and leaves from unreferenced.
func liftingReplaceAll(from Value, to Value) {
	var operands []*Value
	fromReferrers := from.Referrers()
	toReferrers := to.Referrers()
	for _, referrer := range *fromReferrers {
		if composite, isComposite := referrer.(*CompositeValue); isComposite {
			// Walked directly, since a composite value may hold very many operands.
			for index, value := range composite.Values {
				if value == from {
					composite.Values[index] = to
				}
			}
		} else {
			operands = referrer.Operands(operands[:0])
			for _, operand := range operands {
				if *operand != nil && *operand == from {
					*operand = to
				}
			}
		}
		if toReferrers != nil {
			*toReferrers = append(*toReferrers, referrer)
		}
	}
	*fromReferrers = nil
}

// liftingReplace moves instruction's uses of from to to, as go/ir's replace does.
func liftingReplace(instruction Instruction, from Value, to Value) {
	matched := false
	for _, operand := range instruction.Operands(nil) {
		if *operand == from {
			*operand = to
			matched = true
		}
	}
	if !matched {
		return
	}
	if referrers := to.Referrers(); referrers != nil {
		*referrers = append(*referrers, instruction)
	}
	if referrers := from.Referrers(); referrers != nil {
		*referrers = liftingRemoveReferrer(*referrers, instruction)
	}
}

// liftingRemoveReferrer is referrers without instruction, as go/ir's removeInstr makes it.
func liftingRemoveReferrer(referrers []Instruction, instruction Instruction) []Instruction {
	return slices.DeleteFunc(referrers, func(referrer Instruction) bool { return referrer == instruction })
}

// liftingAddToOperandsReferrers adds instruction to the referrers of each of its operands.
func liftingAddToOperandsReferrers(instruction Instruction) {
	for _, operand := range instruction.Operands(nil) {
		if referrers := (*operand).Referrers(); referrers != nil {
			*referrers = append(*referrers, instruction)
		}
	}
}
