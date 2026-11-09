package intermediate_representation

// The lifting options the external tests set on a Program before building it (lifting.go). Compiled into the
// package's tests only. Each entry is deleted when the test that stored it ends, so the map holds nothing a
// finished test set.

import (
	"go/types"
	"testing"

	"github.com/system-inc/cohere/static_single_assignment"
)

// BuildUnliftedForTest builds program's functions unlifted, as go/ir's NaiveForm builds them, until test ends.
func BuildUnliftedForTest(test testing.TB, program *Program) {
	liftingStoreForTest(test, program, liftingOptions{unlifted: true})
}

// VerifyLiftingForTest hands verify the core's verdict on every round of lifting each of program's functions,
// until test ends.
func VerifyLiftingForTest(test testing.TB, program *Program,
	verify func(function *Function, violations []static_single_assignment.StaticSingleAssignmentViolation,
		incomplete []string, places int)) {
	liftingStoreForTest(test, program, liftingOptions{verify: verify})
}

// DropStoresFromPlacedViewForTest plants a mutant in program's lifting until test ends: each block's placed view
// leaves out its Stores, which the core then never sees define. verify is handed what VerifyLiftingForTest hands
// it.
func DropStoresFromPlacedViewForTest(test testing.TB, program *Program,
	verify func(function *Function, violations []static_single_assignment.StaticSingleAssignmentViolation,
		incomplete []string, places int)) {
	liftingStoreForTest(test, program, liftingOptions{verify: verify, placedDropsStores: true})
}

// SkipStoreDefinitionForTest plants a mutant in program's lifting until test ends: in each round, the first Store
// it would lift defines nothing, so the value stored there reaches no Load.
func SkipStoreDefinitionForTest(test testing.TB, program *Program) {
	liftingStoreForTest(test, program, liftingOptions{skipStoreDefinition: true})
}

// DropDebugReferenceForTest plants a mutant in program's expression maps until test ends: each function's first
// debug reference goes unrecorded.
func DropDebugReferenceForTest(test testing.TB, program *Program) {
	liftingStoreForTest(test, program, liftingOptions{dropDebugReference: true})
}

// UnaliasDeeplyForTest is unaliasDeeply, for the tests of the instance names it makes canonical.
func UnaliasDeeplyForTest(given types.Type) types.Type { return unaliasDeeply(given) }

// HasLiftingOptionsForTest reports whether program has options stored, for the test that the default path has
// none.
func HasLiftingOptionsForTest(program *Program) bool {
	_, isStored := liftingOptionsByProgram.Load(program)
	return isStored
}

func liftingStoreForTest(test testing.TB, program *Program, options liftingOptions) {
	liftingOptionsByProgram.Store(program, options)
	test.Cleanup(func() { liftingOptionsByProgram.Delete(program) })
}
