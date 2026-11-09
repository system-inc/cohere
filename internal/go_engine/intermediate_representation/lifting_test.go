package intermediate_representation_test

// Lifting, form by form (#ee33njz): each Go control-flow form, lifted, leaves no Allocation lifting could still
// lift, and the shared core's verifier passes on every round of every function.

import (
	"fmt"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/static_single_assignment"
)

// liftingForms are small packages, one control-flow form each.
var liftingForms = map[string]string{
	"if": `package form
func f(condition bool) int {
	value := 1
	if condition {
		value = 2
	} else {
		value = 3
	}
	return value
}`,
	"for": `package form
func f(limit int) int {
	sum := 0
	for index := 0; index < limit; index++ {
		sum += index
	}
	return sum
}`,
	"range over a slice and an array": `package form
func f(values []int, array [3]int) int {
	sum := 0
	for index, value := range values {
		sum += index * value
	}
	for _, value := range array {
		sum += value
	}
	return sum
}`,
	"range over a map, a string and a channel": `package form
func f(table map[string]int, text string, channel chan int) int {
	sum := 0
	for key, value := range table {
		sum += len(key) + value
	}
	for offset, character := range text {
		sum += offset + int(character)
	}
	for value := range channel {
		sum += value
	}
	return sum
}`,
	"range over an integer and a function": `package form
import "iter"
func sequence(limit int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for index := range limit {
			if !yield(index) {
				return
			}
		}
	}
}
func f() int {
	sum := 0
	for value := range sequence(5) {
		if value == 3 {
			break
		}
		sum += value
	}
	return sum
}`,
	"switch and fallthrough": `package form
func f(value int) int {
	result := 0
	switch {
	case value > 10:
		result = 1
		fallthrough
	case value > 5:
		result += 2
	default:
		result = 3
	}
	return result
}`,
	"type switch": `package form
func f(any interface{}) int {
	result := 0
	switch value := any.(type) {
	case int:
		result = value
	case string, []byte:
		result = 1
	}
	return result
}`,
	"select": `package form
func f(channel chan int, done chan struct{}) int {
	result := 0
	select {
	case value := <-channel:
		result = value
	case channel <- result:
		result = 1
	case <-done:
	default:
		result = 2
	}
	return result
}`,
	"goto and labels": `package form
func f(limit int) int {
	total := 0
outer:
	for {
		for {
			if total > limit {
				break outer
			}
			total += 3
			continue outer
		}
	}
	if total < 0 {
		goto finish
	}
	total *= 2
finish:
	return total
}`,
	"defer, panic and recover": `package form
func f(fail bool) (result int) {
	defer func() {
		if recover() != nil {
			result = -1
		}
	}()
	local := 2
	if fail {
		panic("failed")
	}
	result = local
	return result
}`,
	"closures and free variables": `package form
func f() int {
	captured := 1
	read := func() int { return captured }
	captured = 2
	local := 3
	return read() + local
}`,
	"method values": `package form
type counter struct{ value int }
func (c counter) get() int { return c.value }
func f(c counter) int {
	get := c.get
	expression := counter.get
	return get() + expression(c)
}`,
	"generics in generic form": `package form
func last[T any](values []T) (result T) {
	for _, value := range values {
		result = value
	}
	return result
}
func f() int { return last([]int{1, 2}) }`,
	"an address escaping partway, split": `package form
func escape(*int)
func f(condition bool) int {
	value := 32
	if condition {
		println(value)
		escape(&value)
		println(value)
	} else {
		println(value)
	}
	return value
}`,
	"a pointer whose lifting makes its pointee liftable": `package form
func f() int {
	value := 1
	pointer := &value
	*pointer = 2
	return *pointer
}`,
}

// TestLiftingLeavesNothingLiftableAndPassesTheVerifier builds each form lifted, with the verifier on.
func TestLiftingLeavesNothingLiftableAndPassesTheVerifier(test *testing.T) {
	test.Parallel()
	for name, source := range liftingForms {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			built, violations, rounds := liftingBuild(test, source)
			if rounds == 0 {
				test.Errorf("no round of lifting ran in %q, so nothing was checked", name)
			}
			for _, violation := range violations {
				test.Errorf("the verifier: %s", violation)
			}
			for _, function := range differentialOurSourceFunctions(built) {
				for _, block := range function.Blocks {
					for _, instruction := range block.Instructions {
						allocation, isAllocation := instruction.(*intermediate_representation.Allocation)
						if isAllocation && liftingStillLiftable(function, allocation) {
							test.Errorf("%s keeps a liftable Allocation in block %d",
								function.RelativeString(nil), block.Index)
						}
					}
				}
			}
		})
	}
}

// liftingBuild type-checks source and builds it lifted, the verifier on, returning the package, the verifier's
// violations, and how many rounds of lifting ran.
func liftingBuild(test *testing.T, source string) (*intermediate_representation.Package, []string, int) {
	test.Helper()
	unit := differentialCheck(test, "form", source)
	program := intermediate_representation.NewProgram(unit.fileSet)
	var guard sync.Mutex
	var violations []string
	rounds := 0
	intermediate_representation.VerifyLiftingForTest(test, program, func(function *intermediate_representation.Function,
		found []static_single_assignment.StaticSingleAssignmentViolation) {
		guard.Lock()
		defer guard.Unlock()
		rounds++
		for _, violation := range found {
			violations = append(violations, fmt.Sprintf("%s: %s", function.RelativeString(nil), violation.Detail))
		}
	})
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	built.Build()
	return built, violations, rounds
}

// liftingStillLiftable reports whether lifting left allocation behind though nothing but loads from it and stores
// of something else to it read it, outside the Recover block, whose results go/ir keeps in memory.
func liftingStillLiftable(function *intermediate_representation.Function,
	allocation *intermediate_representation.Allocation) bool {
	referrers := *allocation.Referrers()
	if len(referrers) == 0 {
		return false
	}
	for _, referrer := range referrers {
		if function.Recover != nil && referrer.Block() == function.Recover {
			return false
		}
		switch referrer := referrer.(type) {
		case *intermediate_representation.Load:
		case *intermediate_representation.Store:
			if referrer.Value == intermediate_representation.Value(allocation) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// TestLiftingIsTheDefault builds a program no test set options on, as the engine builds one, and holds it lifted:
// the variable an if assigns on both arms is a phi, and no Allocation is left.
func TestLiftingIsTheDefault(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "form", liftingForms["if"])
	program := intermediate_representation.NewProgram(unit.fileSet)
	built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	built.Build()
	if intermediate_representation.HasLiftingOptionsForTest(program) {
		test.Fatal("the program carries lifting options no test set")
	}
	phis := 0
	for _, block := range built.Function("f").Blocks {
		phis += len(block.Phis())
		for _, instruction := range block.Instructions {
			if _, isAllocation := instruction.(*intermediate_representation.Allocation); isAllocation {
				test.Errorf("f keeps an Allocation in block %d, so the default build didn't lift", block.Index)
			}
		}
	}
	if phis != 1 {
		test.Errorf("f holds %d phis, want the one that merges the two arms", phis)
	}
}
