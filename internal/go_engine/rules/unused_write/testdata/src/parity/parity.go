// Package parity holds shapes where staticcheck's intermediate representation is built differently from x/tools'
// single assignment form, or might be, and where the port and x/tools' unusedwrite still agree finding for finding.
// The differential test runs both over each and compares.
package parity

type pair struct{ left, right int }

type outer struct {
	pair
	name string
}

type generic[T any] struct{ value T }

// A struct literal compiled into the variable it initializes, then one field written and only that field read.
func literalThenField() int {
	value := pair{left: 1}
	value.right = 2
	return value.right
}

// The same, positional.
func positionalLiteralThenField() int {
	value := pair{1, 2}
	value.right = 3
	return value.right
}

// A pointer to a literal, one field written.
func pointerLiteral() int {
	value := &pair{left: 1}
	value.right = 2
	return value.right
}

// An array literal, only its length read.
func arrayLiteral() int {
	values := [3]int{1, 2, 3}
	return len(values)
}

// An array partly given by its literal.
func partialArrayLiteral() int {
	values := [3]int{1: 5}
	return len(values)
}

// An element written at an index the function computes.
func computedIndex(index int) {
	var values [4]int
	values[index] = 1
	values[index+1] = 2
}

// A promoted field written through the embedded struct.
func promotedField() {
	var value outer
	value.left = 1
	value.name = "x"
}

// A write in a loop the next iteration reads.
func loopCarried(count int) int {
	var value pair
	total := 0
	for range count {
		total += value.left
		value.left = total
	}
	return total
}

// A value receiver of a generic type, its field written.
func (receiver generic[T]) set(value T) {
	receiver.value = value
}

// An instantiation of it.
func useGeneric() {
	var value generic[int]
	value.set(1)
}

// A closure writing a captured struct's field.
func closure() func() {
	var value pair
	return func() {
		value.left = 1
	}
}

// A range-over-func loop body writing a field of its own copy.
func rangeOverFunction(sequence func(func(pair) bool)) {
	for value := range sequence {
		value.left = 1
	}
}

// A struct deferred into a call after a field is written.
func deferred() {
	var value pair
	value.left = 1
	defer println(value.right)
}

// A compound assignment and an increment on a field.
func compound() {
	var value pair
	value.left += 1
	value.right++
}

// A field of an array element.
func elementField() {
	var values [2]pair
	values[0].left = 1
}

// A struct passed by address.
func escapes(sink func(*pair)) {
	var value pair
	value.left = 1
	sink(&value)
}

// A named result written by field.
func namedResult() (result pair) {
	result.left = 1
	return
}

// A named result written by field, in a function that recovers.
func namedResultRecovering() (result pair) {
	defer func() { _ = recover() }()
	result.left = 1
	return
}

// A switch over a value whose field is written in one case.
func switched(choice int) {
	var value pair
	switch choice {
	case 1:
		value.left = 1
	case 2:
		value.right = 2
	}
}

// An array written whole and then by element.
func wholeThenElement() {
	values := [2]int{}
	values = [2]int{3, 4}
	values[1] = 5
}

// A struct reassigned from a literal.
func reassigned() int {
	value := pair{}
	value = pair{left: 1, right: 2}
	value.left = 3
	return value.left
}

// A select reading into a struct's field.
func selected(channel chan int) {
	var value pair
	select {
	case value.left = <-channel:
	default:
	}
}

// A struct's field written by a multiple assignment.
func multiple() {
	var value pair
	value.left, value.right = 1, 2
}

// A string index lookup on a struct field.
func stringField() byte {
	var value struct{ text string }
	value.text = "x"
	return value.text[0]
}

// A single element written at a parameter's index.
func parameterIndex(index int) {
	var values [4]int
	values[index] = 1
}

// A single element written at a computed index.
func computedIndexAlone(index int) {
	var values [4]int
	values[index+1] = 1
}

// A pointer to a literal with every field given, one written again.
func pointerLiteralRewritten() int {
	value := &pair{left: 1, right: 2}
	value.right = 3
	return value.right
}

// A pointer to a positional literal, nothing read but one field.
func pointerPositionalLiteral() int {
	value := &pair{1, 2}
	return value.right
}

// A pointer to a partial array literal, keyed.
func pointerPartialArrayLiteral() int {
	values := &[3]int{2: 7}
	return len(values)
}

// A partial array literal into a variable, then an element written.
func partialArrayThenElement() {
	values := [3]int{1: 5}
	values[2] = 1
}

// A struct literal into a variable, then a field written.
func literalThenFieldAlone() {
	value := pair{left: 1}
	value.right = 2
}

// A pointer to a literal that escapes.
func pointerLiteralEscapes() *pair {
	value := &pair{left: 1}
	value.right = 2
	return value
}

// A field of a struct literal field.
func nestedLiteral() int {
	value := &outer{pair: pair{left: 1}, name: "x"}
	return value.left
}

// A pointer to a literal assigned to the blank identifier.
func blankLiteral() {
	_ = &pair{left: 1, right: 2}
}

// A struct assigned to the blank identifier after a field is written.
func blankAfterWrite() {
	var value pair
	value.left = 1
	_ = value
}

func (value pair) sum() int { return value.left + value.right }

func (value *pair) reset() { *value = pair{} }

// A promoted value method called after another field is written.
func promotedMethod() int {
	var value outer
	value.name = "x"
	return value.sum()
}

// A pointer method called after a field is written.
func pointerMethod() {
	var value pair
	value.left = 1
	value.reset()
}

// An allocation by new, one field written.
func allocatedByNew() {
	value := new(pair)
	value.left = 1
}

// A write after a return.
func unreachableWrite() {
	var value pair
	return
	value.left = 1
}

// Every element read in a range over the array by value, after one is written.
func rangeByValue() int {
	var values [3]int
	values[0] = 1
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

// A field written in a type switch's case on a struct value.
func typeSwitched(input any) {
	switch value := input.(type) {
	case pair:
		value.left = 1
	}
}

// A field written in a loop, then the struct escapes in a later iteration.
func escapesLater(sink func(*pair), count int) {
	var value pair
	for index := range count {
		if index > 1 {
			sink(&value)
		}
		value.left = index
	}
}

// A struct converted after a field is written.
type renamed pair

func converted() renamed {
	var value pair
	value.left = 1
	return renamed(value)
}

// A struct compared after a field is written.
func compared(other pair) bool {
	var value pair
	value.left = 1
	return value == other
}

// A field's address taken after another field is written.
func addressOfField() *int {
	var value pair
	value.left = 1
	return &value.right
}

// A struct captured by a closure that reads it.
func capturedAndRead() func() int {
	var value pair
	value.left = 1
	return func() int { return value.left }
}

// A struct written in a goroutine's closure.
func inGoroutine() {
	go func() {
		var value pair
		value.left = 1
	}()
}

// A nested array element.
func nestedArray() {
	var grid [2][2]int
	grid[0][1] = 1
}

// A labeled loop writing a field and continuing.
func labeled(count int) {
	var value pair
outer:
	for index := range count {
		for range count {
			value.left = index
			continue outer
		}
	}
}

func position() int { return 1 }

// An index a call returns: both forms name it by its register.
func callIndex() {
	var values [4]int
	values[position()] = 1
}
