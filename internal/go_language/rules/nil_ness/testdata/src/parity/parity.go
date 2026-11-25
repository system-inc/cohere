// Package parity holds shapes where staticcheck's intermediate representation is built differently from x/tools'
// single assignment form, or might be, and where the port and x/tools' nilness still agree finding for finding. The
// differential test runs both over each and compares.
package parity

import (
	"errors"
	"log"
	"os"
	"unsafe"
)

type pair struct{ left, right int }

type inner struct{ value int }

type outer struct {
	*inner
	name string
}

type valueReceiver struct{ value int }

func (receiver valueReceiver) read() int { return receiver.value }

type embedsValue struct{ valueReceiver }

type container struct{ items []int }

// The second type switch's nil case is impossible once the first has returned on nil.
func typeSwitchTwice(value any) {
	switch value.(type) {
	case nil:
		return
	}
	switch value.(type) {
	case nil:
		print(1)
	case int:
		print(2)
	}
}

// The nil case learns the value nil.
func typeSwitchNilCaseLearnsNil(value any) {
	switch value.(type) {
	case int:
		print(1)
	case nil:
		_ = value.(error)
	default:
		print(2)
	}
}

// A case after the nil case learns the value is not nil.
func typeSwitchLaterCaseLearnsNonNil(value any) {
	switch value.(type) {
	case nil:
		print(1)
	case error:
		if value == nil {
			print(2)
		}
	}
}

// The default is tested last, after the nil case, wherever it is written.
func typeSwitchDefaultFirst(value any) {
	switch value.(type) {
	default:
		if value == nil {
			print(1)
		}
	case nil:
		print(2)
	}
}

// A case before the nil case learns nothing.
func typeSwitchEarlierCase(value any) {
	switch value.(type) {
	case error:
		if value == nil {
			print(1)
		}
	case nil:
		print(2)
	}
}

// The variable a nil case binds is nil.
func typeSwitchBoundNil(value error) {
	switch bound := value.(type) {
	case nil:
		print(bound.Error())
	}
}

// A clause of two cases, nil among them, learns nothing.
func typeSwitchNilAmongTwo(value any) {
	switch value.(type) {
	case nil, int:
		_ = value.(error)
	}
}

// Every case after the nil case returns to one block, which learns the value is not nil.
func typeSwitchJoinAfterLaterCases(value any) {
	switch value.(type) {
	case nil:
		return
	case int:
		print(1)
	case string:
		print(2)
	}
	if value == nil {
		print(3)
	}
}

// The value is known nil, so the nil case is taken always and every later case never.
func typeSwitchKnownNil() {
	var value any
	switch value.(type) {
	case int:
		print(1)
	case nil:
		print(2)
	default:
		var pointer *int
		print(*pointer)
	}
}

// The value is known not nil, so the nil case is never taken.
func typeSwitchKnownNonNil() {
	value := any(1)
	switch value.(type) {
	case nil:
		var pointer *int
		print(*pointer)
	case int:
		print(1)
	}
}

// A known nil type switch inside a loop, and a block after it reached from two later cases.
func typeSwitchKnownNilJoin(values []int) {
	for range values {
		var value any
		switch value.(type) {
		case nil:
			continue
		case int:
			print(1)
		case string:
			print(2)
		}
		var pointer *int
		print(*pointer)
	}
}

// A known nil value whose nil case shares a clause with another type.
func typeSwitchKnownNilShared() {
	var value error
	switch value.(type) {
	case interface{ Unwrap() error }, nil:
		print(1)
	case interface{ Is(error) bool }:
		var pointer *int
		print(*pointer)
	}
}

// A type switch in a function a deferred recovery gives a second entry.
func typeSwitchWithRecovery(value any) {
	defer func() {
		_ = recover()
	}()
	switch value.(type) {
	case nil:
		return
	case int:
		print(1)
	}
	if value == nil {
		print(2)
	}
}

// A struct literal assigned through a nil pointer.
func storeStructLiteral(pointer *pair) {
	if pointer == nil {
		*pointer = pair{left: 1}
	}
}

// An array literal that gives some elements, through a nil pointer.
func storePartialArrayLiteral(pointer *[3]int) {
	if pointer == nil {
		*pointer = [3]int{1: 2}
	}
}

// An array literal that gives every element, through a nil pointer.
func storeFullArrayLiteral(pointer *[2]int) {
	if pointer == nil {
		*pointer = [2]int{1, 2}
	}
}

// An empty struct literal, through a nil pointer.
func storeEmptyStructLiteral(pointer *pair) {
	if pointer == nil {
		*pointer = pair{}
	}
}

// A slice literal, through a nil pointer.
func storeSliceLiteral(pointer *[]int) {
	if pointer == nil {
		*pointer = []int{1}
	}
}

// A map literal, through a nil pointer.
func storeMapLiteral(pointer *map[int]int) {
	if pointer == nil {
		*pointer = map[int]int{1: 2}
	}
}

// A parenthesized dereference written.
func storeParenthesized(pointer *int) {
	if pointer == nil {
		(*pointer) = 1
	}
}

// An increment through a nil pointer.
func storeIncrement(pointer *int) {
	if pointer == nil {
		(*pointer)++
	}
}

// An operator assignment through a nil pointer.
func storeOperatorAssignment(pointer *int) {
	if pointer == nil {
		*pointer += 2
	}
}

// A parallel assignment, one of whose locations is through a nil pointer.
func storeParallel(pointer *int) int {
	other := 0
	if pointer == nil {
		other, *pointer = 1, 2
	}
	return other
}

// Two values of a call, one written through a nil pointer.
func storeFromCall(pointer *int) int {
	other := 0
	if pointer == nil {
		other, *pointer = two()
	}
	return other
}

func two() (int, int) { return 1, 2 }

// A struct literal through a nil pointer in a parallel assignment.
func storeParallelLiteral(pointer *pair) int {
	other := 0
	if pointer == nil {
		other, *pointer = 1, pair{right: 2}
	}
	return other
}

// A map written by an operator assignment, an increment and a parallel assignment.
func mapUpdates(values map[string]int) int {
	other := 0
	if values == nil {
		values["one"] += 1
		values["two"]++
		other, values["three"] = 1, 2
		values["four"], other = 3, 4
	}
	return other
}

// A nil slice ranged over directly and through a slice expression, and a nil pointer to an array ranged over.
func rangeShapes(values []int, array *[2]int) {
	if values == nil {
		for _, value := range values[:] {
			print(value)
		}
		for _, value := range values {
			print(value)
		}
	}
	if array == nil {
		for _, value := range array {
			print(value)
		}
	}
}

// A nil pointer read whole, its field read, and its array indexed and sliced.
func loads(pointer *int, structure *pair, array *[3]int) {
	if pointer == nil {
		print(*pointer)
	}
	if structure == nil {
		print(structure.left)
		structure.right++
	}
	if array == nil {
		print(array[1])
		print(array[:])
		print(len(array))
	}
}

// A field reached through a nil embedded pointer, and a method promoted through it.
func embedded(value *outer) {
	if value == nil {
		print(value.value)
	}
	if value != nil && value.inner == nil {
		print(value.value)
	}
}

// A method with a value receiver, promoted from a field embedded by value, called through a nil pointer.
func valueMethods(embedding *embedsValue) {
	if embedding == nil {
		print(embedding.read())
	}
}

// Calls of a nil function, deferred and started.
func calls(function func(), method interface{ run() }) {
	if function == nil {
		function()
		defer function()
		go function()
	}
	if method == nil {
		method.run()
		defer method.run()
		go method.run()
		run := method.run
		run()
	}
}

// A nil channel sent to and received from, alone and in a one-case select.
func channels(channel chan int) {
	if channel == nil {
		value, ok := <-channel
		print(value, ok)
		select {
		case channel <- 1:
		}
		select {
		case received := <-channel:
			print(received)
		}
		select {
		case channel <- 2:
		case <-channel:
		}
	}
}

// Panics with values whose nilness is known.
func panics(choice int, pointer *int, failure error) {
	switch choice {
	case 0:
		panic(any(nil))
	case 1:
		if pointer == nil {
			panic(pointer)
		}
	case 2:
		if failure == nil {
			panic(failure)
		}
	}
}

// Comparisons whose outcome an interface built from a concrete value or a nil interface decides.
func comparisons(pointer *int, failure error) {
	var boxed any = pointer
	if boxed == nil {
		print(1)
	}
	var nothing error
	var widened any = nothing
	if widened == nil {
		print(2)
	}
	if failure != nil {
		return
	}
	var wider any = failure
	if nil == wider {
		print(3)
	}
	var address unsafe.Pointer
	if address == nil {
		print(4)
	}
	function := two
	if function == nil {
		print(5)
	}
}

// A switch on a pointer with a nil case, and a switch with no tag.
func switches(pointer *int) {
	var known *int
	switch known {
	case nil:
		print(1)
	}
	switch {
	case pointer == nil:
		print(*pointer)
	case pointer != nil && pointer == nil:
		print(2)
	}
	switch pointer {
	case nil:
		print(*pointer)
	}
}

// A type assertion whose failure leaves the pointer nil, negated and on a value already asserted.
func assertions(value any) {
	pointer, ok := value.(*int)
	if !ok {
		print(*pointer)
	}
	slice, isSlice := value.([]int)
	if !isSlice {
		print(slice[0])
	}
	number, isNumber := value.(int)
	if !isNumber {
		print(number)
	}
}

// A function that never returns ends the path that would reach the comparison.
func neverReturns(failure error) {
	if failure != nil {
		os.Exit(1)
	}
	if failure != nil {
		print(1)
	}
	if failure == nil {
		log.Fatal("no failure")
	}
	if failure == nil {
		print(2)
	}
}

func fail() { panic(errors.New("failed")) }

// A local function that only panics ends the path too.
func localNeverReturns(failure error) {
	if failure != nil {
		fail()
	}
	if failure != nil {
		print(1)
	}
}

// A closure reads a captured variable, and one it assigns.
func closures() {
	var pointer *int
	read := func() int {
		return *pointer
	}
	print(read())
	var assigned *int
	func() {
		assigned = new(int)
	}()
	if assigned == nil {
		print(*assigned)
	}
	var local *int
	func() {
		if local == nil {
			print(*local)
		}
	}()
}

// A nil function ranged over.
func rangeOverFunction() {
	var sequence func(func(int) bool)
	for value := range sequence {
		print(value)
	}
}

// A nil map ranged over by key, and a nil slice converted to an array.
func conversions(values []int, table map[int]int) {
	if table == nil {
		for key := range table {
			print(key)
		}
	}
	if values == nil {
		array := [0]int(values)
		print(len(array))
		pointer := (*[0]int)(values)
		print(pointer == nil)
	}
}

// A generic value compared with its own zero, and a nil pointer of a type parameter's type dereferenced.
func generic[T any, P ~*T](pointer P) {
	if pointer == nil {
		print(*pointer)
	}
	var zero P
	if zero != nil {
		print(1)
	}
}

// A comparison in a short-circuit chain learns along each edge.
func shortCircuit(first, second *int) {
	if first == nil || second == nil {
		return
	}
	if first == nil {
		print(1)
	}
	if first != nil && second == nil {
		print(2)
	}
}

// A labelled loop and a goto between comparisons.
func controlFlow(pointer *int) {
outer:
	for {
		if pointer == nil {
			break outer
		}
		if pointer == nil {
			continue
		}
		goto done
	}
	if pointer == nil {
		print(*pointer)
	}
done:
	print(1)
}

// A deferred recovery gives the function a second entry, which the original does not visit.
func recovered(pointer *int) (result int) {
	defer func() {
		if recover() != nil {
			result = 1
		}
	}()
	if pointer == nil {
		return *pointer
	}
	return 0
}
