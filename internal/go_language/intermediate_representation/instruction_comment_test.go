package intermediate_representation_test

// The comments the builder and lifting label instructions with (#z844663): each kind that carries one keeps it in a
// field of its own, since anInstruction no longer holds one for every kind, and every other kind reads none.

import (
	"reflect"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
	"github.com/system-inc/cohere/modules/static_single_assignment"
)

// instructionCommentSource makes each labeled kind once, lifted: the phi of a `&&` value, the closure a range over a
// function calls with "yield", and the Allocation, Load and Store that split value, whose address escapes partway.
const instructionCommentSource = `package comments
func escape(*int)
func sequence(yield func(int) bool) {}
func f(left bool, right bool) int {
	value := 32
	println(value)
	both := left && right
	if both {
		escape(&value)
	}
	sum := 0
	for each := range sequence {
		sum += each
	}
	return value + sum
}`

// TestInstructionCommentsStayOnTheKindsThatCarryThem builds instructionCommentSource lifted and reads every
// instruction's comment, in each round of lifting before the round removes what it lifted, the split's Load among
// them, and in the built functions: each labeled kind is found with its label, and no other kind has one.
func TestInstructionCommentsStayOnTheKindsThatCarryThem(test *testing.T) {
	test.Parallel()
	labeled := map[string]bool{"Allocation": true, "Load": true, "Store": true, "Phi": true, "MakeClosure": true}
	want := map[string]string{"Allocation": "split alloc", "Load": "split alloc", "Store": "split alloc",
		"Phi": "&&", "MakeClosure": "yield"}
	var guard sync.Mutex
	found := map[string]bool{}
	read := func(function *intermediate_representation.Function) {
		guard.Lock()
		defer guard.Unlock()
		for _, block := range function.Blocks {
			for _, instruction := range block.Instructions {
				kind := reflect.TypeOf(instruction).Elem().Name()
				comment := instruction.Comment()
				if !labeled[kind] && comment != "" {
					test.Errorf("%s in %s carries the comment %q, which only a labeled kind has", kind,
						function.RelativeString(nil), comment)
				}
				if labeled[kind] && comment == want[kind] {
					found[kind] = true
				}
			}
		}
	}
	unit := differentialCheck(test, "comments", instructionCommentSource)
	program := intermediate_representation.NewProgram(unit.fileSet)
	intermediate_representation.VerifyLiftingForTest(test, program, func(function *intermediate_representation.Function,
		_ []static_single_assignment.StaticSingleAssignmentViolation, _ []string, _ int) {
		read(function)
	})
	built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	built.Build()
	for _, function := range differentialOurSourceFunctions(built) {
		read(function)
	}
	for kind, comment := range want {
		if !found[kind] {
			test.Errorf("no %s carries the comment %q", kind, comment)
		}
	}
}
