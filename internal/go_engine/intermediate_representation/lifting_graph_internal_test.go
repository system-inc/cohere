package intermediate_representation

import (
	"reflect"
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

// liftingScratchRound prepares and resolves, in scratch, a round over a function of count blocks, each holding one
// Allocation to lift, and returns the Allocations. Nothing stores to one or loads from it, so the core has nothing
// to name and the round goes from the prepare straight to the resolve.
//
// Mutates: scratch its tables hold the round
func liftingScratchRound(scratch *liftingScratch, count int) []*Allocation {
	function := &Function{}
	var allocations []*Allocation
	for index := range count {
		allocation := &Allocation{}
		block := &BasicBlock{Index: index, Instructions: []Instruction{allocation}}
		allocation.setBlock(block)
		function.Blocks = append(function.Blocks, block)
		allocations = append(allocations, allocation)
	}
	liftingResolve(liftingPrepare(function, allocations, liftingOptions{}, scratch), scratch)
	return allocations
}

// liftingPanicked runs run and reports whether it panicked.
func liftingPanicked(run func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	run()
	return false
}

// TestReleaseClearsTheTailALargerRoundLeft takes one scratch through a function's large round and then its small
// one, releases it, and runs the next function's round in it, then reads blockValues, order and definitions to
// each array's capacity: past what the next function used, nothing of the earlier one is left. release clears
// those three only as far as the function used them (#w5dbvdm), so what it clears to has to be the longest round
// since the last release, not the last round, whose tables stop short of the large one's tail.
func TestReleaseClearsTheTailALargerRoundLeft(test *testing.T) {
	test.Parallel()
	const large = 32
	const small = 2
	var scratch liftingScratch
	earlier := liftingScratchRound(&scratch, large)
	liftingScratchRound(&scratch, small)
	// The small round left the large one's tail in each array, or release would have nothing to clear there.
	if scratch.definitions[:large][large-1].zero != earlier[large-1] ||
		scratch.blockValues[:large][large-1].block != earlier[large-1].block ||
		scratch.order[:large][large-1].block != earlier[large-1].block {
		test.Fatal("after the small round, the large round's last block is not at the end of each array")
	}
	scratch.release()

	next := liftingScratchRound(&scratch, small)
	if cap(scratch.definitions) < large || cap(scratch.blockValues) < large || cap(scratch.order) < large {
		test.Fatalf("the next function's round runs in arrays of %d, %d and %d, not the %d the large round grew",
			cap(scratch.definitions), cap(scratch.blockValues), cap(scratch.order), large)
	}
	for index, allocation := range next {
		if scratch.definitions[index].zero != allocation || scratch.blockValues[index].block != allocation.block {
			test.Errorf("entry %d is not the next function's own", index)
		}
	}
	for offset, definition := range scratch.definitions[small:cap(scratch.definitions)] {
		if definition != (liftingDefinition{}) {
			test.Errorf("definition %d, past the next function's %d, still holds the earlier function's",
				small+offset, small)
			break
		}
	}
	for offset, block := range scratch.blockValues[small:cap(scratch.blockValues)] {
		if !reflect.ValueOf(block).IsZero() {
			test.Errorf("block value %d, past the next function's %d, still holds the earlier function's",
				small+offset, small)
			break
		}
	}
	for offset, block := range scratch.order[small:cap(scratch.order)] {
		if block != nil {
			test.Errorf("order entry %d, past the next function's %d, still holds the earlier function's",
				small+offset, small)
			break
		}
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
