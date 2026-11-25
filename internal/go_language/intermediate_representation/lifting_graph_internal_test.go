package intermediate_representation

import (
	"reflect"
	"slices"
	"testing"

	"github.com/system-inc/cohere/static_single_assignment"
)

// TestMintGrowsTheTableTheResolveIsSizedBy mints identifiers past the declarations a function starts with, and
// holds each to a place in lifting.declarations: liftingResolve sizes its dense tables by that slice's length, so
// an identifier Mint handed out without growing it would index past them (#beapx0q).
func TestMintGrowsTheTableTheResolveIsSizedBy(test *testing.T) {
	test.Parallel()
	lifting := &liftingFunction{
		declarations: []static_single_assignment.DeclarationId{0, 1},
		allocations:  []*Allocation{{}, {}},
	}
	var minted []static_single_assignment.IdentifierId
	for round := range 40 {
		minted = append(minted, liftingGraph{}.Mint(lifting, static_single_assignment.IdentifierId(round%2)))
	}
	if len(lifting.declarations) != 42 {
		test.Fatalf("after 40 mints over 2 declarations, the table holds %d identifiers, want 42",
			len(lifting.declarations))
	}
	for index, identifier := range minted {
		if int(identifier) >= len(lifting.declarations) {
			test.Fatalf("mint %d handed out identifier %d past the table's %d", index, identifier,
				len(lifting.declarations))
		}
		want := static_single_assignment.DeclarationId(index % 2)
		if got := (liftingGraph{}).Declaration(lifting, identifier); got != want {
			test.Errorf("identifier %d declares %d, want %d", identifier, got, want)
		}
	}
	definitions := make([]liftingDefinition, len(lifting.declarations))
	if last := minted[len(minted)-1]; int(last) >= len(definitions) {
		test.Errorf("the last identifier minted, %d, is past a resolve table of %d", last, len(definitions))
	}
}

// liftingScratchFunction is a function of count blocks, each holding one Allocation, a Store of a constant to it and
// a Load from it, so a round lifts every Allocation and leaves the blocks empty.
func liftingScratchFunction(count int) *Function {
	function := &Function{}
	for index := range count {
		block := &BasicBlock{Index: index, parent: function}
		allocation := &Allocation{}
		store := &Store{Address: allocation, Value: &Constant{}}
		load := &Load{X: allocation}
		allocation.referrers = []Instruction{store, load}
		block.Instructions = []Instruction{allocation, store, load}
		for _, instruction := range block.Instructions {
			instruction.setBlock(block)
		}
		function.Blocks = append(function.Blocks, block)
	}
	return function
}

// liftingScratchLift lifts a function of count blocks in scratch, round after round until one lifts nothing, as
// liftFunction does but for the pool and the release: the first round lifts count Allocations and the last none.
//
// Mutates: scratch its tables hold the function's rounds
func liftingScratchLift(scratch *liftingScratch, count int) {
	function := liftingScratchFunction(count)
	for liftingRound(function, liftingOptions{}, scratch) {
	}
}

// liftingScratchHeldPast is the index of the first element of array past prefix, read to array's capacity, that is
// not zero, or -1 when every one is.
func liftingScratchHeldPast[Element any](array []Element, prefix int) int {
	for offset, element := range array[prefix:cap(array)] {
		if !reflect.ValueOf(element).IsZero() {
			return prefix + offset
		}
	}
	return -1
}

// liftingScratchTail is what liftingScratchHeldPast finds of one of scratch's arrays.
type liftingScratchTail struct {
	array string
	held  int
}

// liftingScratchTails reads each array release clears only as far as the function used it, past a function of
// count blocks: the first count entries of each, and of definitions its whole length, the core minting an
// identifier for each Store beyond the Allocations' own.
func liftingScratchTails(scratch *liftingScratch, count int) []liftingScratchTail {
	return []liftingScratchTail{
		{array: "allocations", held: liftingScratchHeldPast(scratch.allocations, count)},
		{array: "lifting's blocks", held: liftingScratchHeldPast(scratch.lifting.blocks, count)},
		{array: "blockValues", held: liftingScratchHeldPast(scratch.blockValues, count)},
		{array: "order", held: liftingScratchHeldPast(scratch.order, count)},
		{array: "definitions", held: liftingScratchHeldPast(scratch.definitions, len(scratch.definitions))},
	}
}

// liftingPanicked runs run and reports whether it panicked.
func liftingPanicked(run func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	run()
	return false
}

// TestReleaseClearsTheTailALargerRoundLeft takes one scratch through a large function's rounds and then a small
// one's, releases it, and lifts the next function in it, then reads allocations, lifting's blocks, blockValues,
// order and definitions to each array's capacity: past what the next function used, nothing of the earlier ones is
// left. release clears those five only as far as they were used (#w5dbvdm), so what it clears to has to be the
// longest round since the last release, not the last round, whose tables stop short of the large one's tail. The
// two functions before the release stand for one function's larger and smaller rounds, a function built here
// lifting everything in its first.
func TestReleaseClearsTheTailALargerRoundLeft(test *testing.T) {
	test.Parallel()
	const large = 32
	const small = 2
	var scratch liftingScratch
	liftingScratchLift(&scratch, large)
	definitionsOfLarge := len(scratch.definitions)
	liftingScratchLift(&scratch, small)
	// The small function left the large one's tail in each array, or release would have nothing to clear there.
	for _, tail := range liftingScratchTails(&scratch, small) {
		if tail.held == -1 {
			test.Fatalf("before the release, %s holds nothing of the large function past the small one's",
				tail.array)
		}
	}
	scratch.release()

	liftingScratchLift(&scratch, small)
	if cap(scratch.allocations) < large || cap(scratch.lifting.blocks) < large || cap(scratch.blockValues) < large ||
		cap(scratch.order) < large || cap(scratch.definitions) < definitionsOfLarge {
		test.Fatalf("the next function lifts in arrays of %d, %d, %d, %d and %d, not the ones the large one grew",
			cap(scratch.allocations), cap(scratch.lifting.blocks), cap(scratch.blockValues), cap(scratch.order),
			cap(scratch.definitions))
	}
	if len(scratch.lifting.blocks) != small || len(scratch.blockValues) != small || len(scratch.order) != small ||
		len(scratch.definitions) >= definitionsOfLarge {
		test.Fatalf("the next function used %d, %d, %d and %d entries, want %d of each and fewer than %d definitions",
			len(scratch.lifting.blocks), len(scratch.blockValues), len(scratch.order), len(scratch.definitions),
			small, definitionsOfLarge)
	}
	for _, tail := range liftingScratchTails(&scratch, small) {
		if tail.held != -1 {
			test.Errorf("%s entry %d, past what the next function used, still holds an earlier function's",
				tail.array, tail.held)
		}
	}
}

// TestReleaseClearsTheAllocationsOfARoundThatLifted releases a scratch right after a round that lifted, where
// allocations still holds that round's and no later round has taken their length into allocationsUsed: release
// clears to the longer of the mark and the array's own length. liftFunction's last round lifts nothing, so on its
// path the mark alone covers every round, and this pins release for a caller that stops sooner.
func TestReleaseClearsTheAllocationsOfARoundThatLifted(test *testing.T) {
	test.Parallel()
	const count = 4
	var scratch liftingScratch
	if !liftingRound(liftingScratchFunction(count), liftingOptions{}, &scratch) || len(scratch.allocations) != count {
		test.Fatalf("the round lifted %d Allocations, want %d", len(scratch.allocations), count)
	}
	scratch.release()
	if held := liftingScratchHeldPast(scratch.allocations, 0); held != -1 {
		test.Errorf("after release, allocations entry %d still holds what the round lifted", held)
	}
}

// TestReleaseClearsWhatAResolveWroteBeforeItPanicked stops a resolve partway: its one Store has a place and no
// address, so the resolve writes the Store into the definitions and panics later, reading the address. It pins that
// release clears what a resolve wrote before it stopped, since definitionsUsed is set before any write. It does not
// pin a pooled scratch: liftFunction never reaches release or the pool when a round panics, so that scratch is
// dropped and no later function sees it.
func TestReleaseClearsWhatAResolveWroteBeforeItPanicked(test *testing.T) {
	test.Parallel()
	var scratch liftingScratch
	store := &Store{}
	lifting := &liftingFunction{
		declarations: []static_single_assignment.DeclarationId{0},
		blocks: []*liftingBlock{{
			block: &BasicBlock{Instructions: []Instruction{store}},
			slots: []liftingSlot{{isPlace: true}},
		}},
	}
	if !liftingPanicked(func() { liftingResolve(lifting, &scratch) }) {
		test.Fatal("the resolve did not panic on the Store's missing address")
	}
	if scratch.definitions[0].store != store {
		test.Fatal("the resolve panicked before it wrote the Store into the definitions")
	}
	scratch.release()
	for index, definition := range scratch.definitions[:cap(scratch.definitions)] {
		if definition != (liftingDefinition{}) {
			test.Errorf("after release, definition %d still holds what the panicking resolve wrote", index)
		}
	}
}

// TestReleaseClearsAScratchWhosePrepareStoppedBeforeItsWalk stops a prepare between its blocks and its walk: the
// Allocation it is to lift sits in no block, so the prepare writes the function's block into blockValues, then
// panics placing the Allocation, and order, which only the walk grows, has no array at all. release clears order
// as far as the blocks the round prepared, and here that is past order's capacity, so it has to stop at the
// capacity rather than panic itself. Like the resolve's, this pins release alone, not a pooled scratch.
func TestReleaseClearsAScratchWhosePrepareStoppedBeforeItsWalk(test *testing.T) {
	test.Parallel()
	var scratch liftingScratch
	block := &BasicBlock{}
	function := &Function{Blocks: []*BasicBlock{block}}
	prepare := func() { liftingPrepare(function, []*Allocation{{}}, liftingOptions{}, &scratch) }
	if !liftingPanicked(prepare) {
		test.Fatal("the prepare did not panic on an Allocation in no block")
	}
	if scratch.blockValues[0].block != block || cap(scratch.order) != 0 {
		test.Fatalf("the prepare stopped with block %p in blockValues and an order of capacity %d, "+
			"want the function's block and no order", scratch.blockValues[0].block, cap(scratch.order))
	}
	if liftingPanicked(scratch.release) {
		test.Fatal("release panicked on an order shorter than the blocks the round prepared")
	}
	if !reflect.ValueOf(scratch.blockValues[0]).IsZero() {
		test.Error("after release, blockValues still holds the block the panicking prepare wrote")
	}
}

// liftingEdgesFunction is a function of count empty blocks joined by edges, each a pair of block indices, added in
// order.
func liftingEdgesFunction(count int, edges [][2]int) *Function {
	function := &Function{}
	for index := range count {
		function.Blocks = append(function.Blocks, &BasicBlock{Index: index, parent: function})
	}
	for _, edge := range edges {
		addEdge(function.Blocks[edge[0]], function.Blocks[edge[1]])
	}
	return function
}

// TestPredecessorViewsAreEachBlocksOwn prepares a function with a loop, 6 predecessors in all, then a line of 3
// blocks, 2 in all, in one scratch released between them, as the pool hands one function's scratch to the next
// (#z844663). Each block of the second sees its own predecessors and no more, the shared array holds the second's 2
// rather than growing past the first's, and an append to one block's view, as the core's graph maintenance makes,
// leaves the next block's predecessors as they were.
func TestPredecessorViewsAreEachBlocksOwn(test *testing.T) {
	test.Parallel()
	var scratch liftingScratch
	looped := liftingEdgesFunction(5, [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}, {3, 4}, {4, 1}})
	liftingPrepare(looped, nil, liftingOptions{}, &scratch)
	scratch.release()
	line := liftingEdgesFunction(3, [][2]int{{0, 1}, {1, 2}})
	lifting := liftingPrepare(line, nil, liftingOptions{}, &scratch)
	for _, block := range lifting.blocks {
		var want []static_single_assignment.BlockId
		for _, predecessor := range block.block.Predecessors {
			want = append(want, static_single_assignment.BlockId(predecessor.Index))
		}
		if got := (liftingGraph{}).Predecessors(block); !slices.Equal(got, want) {
			test.Errorf("block %d's predecessors are %v, want %v", block.block.Index, got, want)
		}
	}
	if len(scratch.predecessors) != 2 {
		test.Errorf("the shared array holds %d predecessors after the line's prepare, want its 2",
			len(scratch.predecessors))
	}
	_ = append((liftingGraph{}).Predecessors(lifting.blocks[1]), 99)
	if got := (liftingGraph{}).Predecessors(lifting.blocks[2]); !slices.Equal(got,
		[]static_single_assignment.BlockId{1}) {
		test.Errorf("after an append to block 1's view, block 2's predecessors are %v, want [1]", got)
	}
}
