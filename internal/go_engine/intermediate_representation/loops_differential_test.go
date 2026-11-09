package intermediate_representation_test

// FindLoops and Terminates against staticcheck's own (#jqbskpv). Each is asked the same question of our IR and
// go/ir's, both lifted, built from one load as irutil_differential_test.go builds them: the same function by its
// place in the package's source functions. The answers must agree everywhere, and the fixture holds functions that
// answer each question both ways, so agreement isn't agreement on a constant.

import (
	"slices"
	"testing"

	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// loopsFixture is the package FindLoops and Terminates are asked about. Its imports are all here, since the
// importer reads this source's alone.
const loopsFixture = `package loops

import "time"

var counter int

func work() { counter++ }

func external()

func noLoop(value int) int {
	if value > 0 {
		return value
	}
	return -value
}

func selfLoop() {
	for {
		counter++
	}
}

func emptyLoop() {
	for {
	}
}

func nested(limit int) int {
	total := 0
	for outer := 0; outer < limit; outer++ {
		for inner := 0; inner < outer; inner++ {
			total += inner
		}
	}
	return total
}

func breaks(values []int) int {
	total := 0
	for _, value := range values {
		if value < 0 {
			break
		}
		total += value
	}
	return total
}

func gotoBack(limit int) int {
	index := 0
again:
	index++
	if index < limit {
		goto again
	}
	return index
}

func panics() {
	panic("no")
}

func ticks() {
	for range time.Tick(time.Second) {
		work()
	}
}

func ticksOrReturns(stop bool) {
	if stop {
		return
	}
	for range time.Tick(time.Second) {
		work()
	}
}
`

// loopsUnits are the units FindLoops and Terminates are asked about: the fixture, and the differential's own units.
func loopsUnits(test *testing.T) []differentialUnit {
	test.Helper()
	fixture := differentialCheck(test, "loops", loopsFixture)
	return append([]differentialUnit{fixture}, differentialTestdataUnits(test)...)
}

// loopsTheirIndexes are the indexes of function's blocks in loop, in order.
func loopsTheirIndexes(function *ir.Function, loop irutil.Loop) []int {
	var indexes []int
	for _, block := range function.Blocks {
		if loop.Has(block) {
			indexes = append(indexes, block.Index)
		}
	}
	return indexes
}

// loopsOurIndexes is loopsTheirIndexes for our IR.
func loopsOurIndexes(function *intermediate_representation.Function,
	loop intermediate_representation.Loop) []int {
	var indexes []int
	for _, block := range function.Blocks {
		if loop.Has(block) {
			indexes = append(indexes, block.Index)
		}
	}
	return indexes
}

// TestFindLoopsMatchesIrutil finds every source function's loops on both sides and requires the same loops, each
// the same blocks, in the same order.
func TestFindLoopsMatchesIrutil(test *testing.T) {
	test.Parallel()
	var looped irutilAnswers
	for _, unit := range loopsUnits(test) {
		theirFunctions, ourFunctions := irutilFunctions(test, unit)
		for index, theirFunction := range theirFunctions {
			ourFunction := ourFunctions[index]
			var theirLoops [][]int
			var ourLoops [][]int
			for _, loop := range irutil.FindLoops(theirFunction) {
				theirLoops = append(theirLoops, loopsTheirIndexes(theirFunction, loop))
			}
			for _, loop := range intermediate_representation.FindLoops(ourFunction) {
				ourLoops = append(ourLoops, loopsOurIndexes(ourFunction, loop))
			}
			if !slices.EqualFunc(theirLoops, ourLoops, slices.Equal[[]int]) {
				test.Errorf("%s: irutil.FindLoops finds %v, ours %v", theirFunction.RelString(nil), theirLoops,
					ourLoops)
			}
			if unit.path == "loops" {
				test.Logf("%s: loops %v", theirFunction.RelString(nil), theirLoops)
			}
			looped.add(len(theirLoops) > 0)
		}
	}
	looped.requireBoth(test, "FindLoops (found a loop)")
}

// TestTerminatesMatchesIrutil asks of every source function whether it terminates, on both sides.
func TestTerminatesMatchesIrutil(test *testing.T) {
	test.Parallel()
	var terminates irutilAnswers
	for _, unit := range loopsUnits(test) {
		theirFunctions, ourFunctions := irutilFunctions(test, unit)
		for index, theirFunction := range theirFunctions {
			theirAnswer := irutil.Terminates(theirFunction)
			ourAnswer := intermediate_representation.Terminates(ourFunctions[index])
			if theirAnswer != ourAnswer {
				test.Errorf("%s: irutil.Terminates is %t, ours %t", theirFunction.RelString(nil), theirAnswer,
					ourAnswer)
			}
			if unit.path == "loops" {
				test.Logf("%s: terminates %t", theirFunction.RelString(nil), theirAnswer)
			}
			terminates.add(theirAnswer)
		}
	}
	terminates.requireBoth(test, "Terminates")
}

// TestTerminatesPanicPredecessorMatchesIrutil asks Terminates of hand-built graphs where a Return block has a Panic
// block among its predecessors, on both sides. The builder never makes one, since a Panic block has no successors
// on either side (go/ir's sanity check requires it), so no fixture reaches irutil's Panic case; these graphs do.
func TestTerminatesPanicPredecessorMatchesIrutil(test *testing.T) {
	test.Parallel()
	var terminates irutilAnswers
	for _, alsoJumps := range []bool{false, true} {
		theirPanic := &ir.BasicBlock{Index: 0, Instrs: []ir.Instruction{&ir.Panic{}}}
		theirReturn := &ir.BasicBlock{Index: 1, Instrs: []ir.Instruction{&ir.Return{}},
			Preds: []*ir.BasicBlock{theirPanic}}
		theirFunction := &ir.Function{Blocks: []*ir.BasicBlock{theirPanic, theirReturn}}
		ourPanic := &intermediate_representation.BasicBlock{Index: 0,
			Instructions: []intermediate_representation.Instruction{&intermediate_representation.Panic{}}}
		ourReturn := &intermediate_representation.BasicBlock{Index: 1,
			Instructions: []intermediate_representation.Instruction{&intermediate_representation.Return{}},
			Predecessors: []*intermediate_representation.BasicBlock{ourPanic}}
		ourFunction := &intermediate_representation.Function{
			Blocks: []*intermediate_representation.BasicBlock{ourPanic, ourReturn}}
		if alsoJumps {
			theirJump := &ir.BasicBlock{Index: 2, Instrs: []ir.Instruction{&ir.Jump{}}}
			theirReturn.Preds = append(theirReturn.Preds, theirJump)
			theirFunction.Blocks = append(theirFunction.Blocks, theirJump)
			ourJump := &intermediate_representation.BasicBlock{Index: 2,
				Instructions: []intermediate_representation.Instruction{&intermediate_representation.Jump{}}}
			ourReturn.Predecessors = append(ourReturn.Predecessors, ourJump)
			ourFunction.Blocks = append(ourFunction.Blocks, ourJump)
		}
		theirAnswer := irutil.Terminates(theirFunction)
		ourAnswer := intermediate_representation.Terminates(ourFunction)
		if theirAnswer != ourAnswer {
			test.Errorf("a Return after a Panic (and a Jump: %t): irutil.Terminates is %t, ours %t", alsoJumps,
				theirAnswer, ourAnswer)
		}
		terminates.add(theirAnswer)
	}
	terminates.requireBoth(test, "Terminates (after a Panic)")
}
