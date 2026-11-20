package intermediate_representation_test

import (
	"testing"
)

// TestBlocksHoldNothingPastTheirInstructions builds the package's fixtures, with the expression map and without, and
// reads every block's instruction array to its capacity: past the instructions the block lists there is nothing.
// A block starts with room for four (#z844663), and fusing, lifting's rewrite and the expression map's strip each
// write in a block's own array, so the room a block doesn't use has to stay empty, or a dropped instruction stays
// reachable from it.
func TestBlocksHoldNothingPastTheirInstructions(test *testing.T) {
	test.Parallel()
	spareBlocks := 0
	for _, unit := range differentialTestdataUnits(test) {
		for _, debug := range []bool{false, true} {
			for _, function := range differentialOurSourceFunctions(differentialBuildOurs(unit, debug)) {
				for _, block := range function.Blocks {
					spare := block.Instructions[len(block.Instructions):cap(block.Instructions)]
					if len(spare) > 0 {
						spareBlocks++
					}
					for offset, instruction := range spare {
						if instruction != nil {
							test.Errorf("%s block %d holds %T at %d, past its %d instructions",
								function.RelativeString(nil), block.Index, instruction,
								len(block.Instructions)+offset, len(block.Instructions))
						}
					}
				}
			}
		}
	}
	if spareBlocks == 0 {
		test.Fatal("no block has room past its instructions, so nothing was read")
	}
	test.Logf("%d blocks with room past their instructions, all of it empty", spareBlocks)
}
