// Package parity holds every shape S1001's query and judgment branch on, each beside its near miss, for the original
// and the port to judge alike.
package parity

import "iter"

type holder struct {
	values []int
	array  [4]int
	inner  *holder
}

type named []int

type namedArray [4]int

type namedPointer *[4]int

func values() []int { return nil }

// rangeWithValue is the first alternative: a key and a value declared, the value stored at the key.
func rangeWithValue() {
	var source, destination []int
	for index, value := range source {
		destination[index] = value
	}
	// Parentheses are looked through everywhere the pattern looks.
	for index, value := range source {
		(destination)[(index)] = (value)
	}
	// A body of one block of one statement is that statement.
	for index, value := range source {
		{
			destination[index] = value
		}
	}
	for index, value := range source {
		{
			{
				destination[index] = value
			}
		}
	}
	// Two statements are no copy.
	for index, value := range source {
		destination[index] = value
		_ = index
	}
	// A block of two statements is none either.
	for index, value := range source {
		{
			destination[index] = value
			_ = value
		}
	}
	// An empty body, and a body of an empty block.
	for index, value := range source {
		_, _ = index, value
	}
	for range source {
		{
		}
	}
	// The value recalled must be the right side's whole.
	for index, value := range source {
		destination[index] = value + 0
	}
	for index, value := range source {
		destination[index] = -value
	}
	// The key recalled must be the index.
	for index, value := range source {
		destination[index+0] = value
	}
	for index, value := range source {
		destination[value] = value
		_ = index
	}
	// Assigned, not declared, the loop is none of the alternatives.
	var index, value int
	for index, value = range source {
		destination[index] = value
	}
	// An operator assignment is no copy.
	for index, value := range source {
		destination[index] += value
	}
	// Two left sides are no copy.
	other := []int{}
	for index, value := range source {
		destination[index], other[index] = value, value
	}
	// A define in the body is no store.
	for index, value := range source {
		destination := destination
		_, _, _ = destination, index, value
	}
	// A blank value is an identifier, and no right side is spelled _, so neither alternative takes it.
	for index, _ := range source {
		destination[index] = source[index]
	}
	// A blank key never stores at the key, and the store at a constant is no copy.
	for _, value := range source {
		destination[0] = value
	}
}

// rangeWithoutValue is the second alternative: a key alone, the source read and stored at it.
func rangeWithoutValue() {
	var source, destination, other []int
	for index := range source {
		destination[index] = source[index]
	}
	for index := range source {
		(destination)[index] = (source)[(index)]
	}
	// A source read that is not the ranged one.
	for index := range source {
		destination[index] = other[index]
	}
	// A read at another index.
	for index := range source {
		destination[index] = source[0]
	}
	// A read that is a call's result.
	for index := range source {
		destination[index] = values()[index]
	}
	// The ranged source by syntax: a selector, and a selector through a pointer.
	var held holder
	for index := range held.values {
		destination[index] = held.values[index]
	}
	for index := range held.inner.values {
		destination[index] = held.inner.values[index]
	}
	for index := range held.inner.values {
		destination[index] = held.values[index]
	}
	// A key alone and a store of something else.
	for index := range source {
		destination[index] = index
	}
}

// counted is the for statement alternative.
func counted() {
	var source, destination, other []int
	for index := 0; index < len(source); index++ {
		destination[index] = source[index]
	}
	for index := 0; (index) < (len)((source)); (index)++ {
		(destination)[index] = (source)[(index)]
	}
	// Integer literals that print as 0.
	for index := 0x0; index < len(source); index++ {
		destination[index] = source[index]
	}
	for index := -0; index < len(source); index++ {
		destination[index] = source[index]
	}
	for index := 00; index < len(source); index++ {
		destination[index] = source[index]
	}
	// Starting anywhere else, or at a zero that is no literal.
	for index := 1; index < len(source); index++ {
		destination[index] = source[index]
	}
	const zero = 0
	for index := zero; index < len(source); index++ {
		destination[index] = source[index]
	}
	for index := int(0); index < len(source); index++ {
		destination[index] = source[index]
	}
	// Assigned, not declared.
	var counter int
	for counter = 0; counter < len(source); counter++ {
		destination[counter] = source[counter]
	}
	// Other conditions.
	for index := 0; index <= len(source); index++ {
		destination[index] = source[index]
	}
	for index := 0; len(source) > index; index++ {
		destination[index] = source[index]
	}
	for index := 0; index < cap(source); index++ {
		destination[index] = source[index]
	}
	for index := 0; index < len(other); index++ {
		destination[index] = source[index]
	}
	for index := 0; index < len(source); {
		destination[index] = source[index]
		index++
	}
	for index := 0; ; index++ {
		destination[index] = source[index]
	}
	// Other post statements.
	for index := 0; index < len(source); index-- {
		destination[index] = source[index]
	}
	for index := 0; index < len(source); index += 1 {
		destination[index] = source[index]
	}
	for index, last := 0, 0; index < len(source); index++ {
		destination[index] = source[index]
		_ = last
	}
	// A len declared in the function is not the builtin.
	{
		len := func([]int) int { return 0 }
		for index := 0; index < len(source); index++ {
			destination[index] = source[index]
		}
	}
	// The source the condition measures, with the parentheses around it, is the one read.
	var held holder
	for index := 0; index < len((held.values)); index++ {
		destination[index] = held.values[index]
	}
	// A source that depends on the key.
	rows := [][]int{}
	for index := 0; index < len(rows[index]); index++ {
		destination[index] = rows[index][index]
	}
}

// labeled loops are reported at the loop, the label left in place.
func labeled() {
	var source, destination []int
first:
	for index, value := range source {
		destination[index] = value
	}
second:
	for index := 0; index < len(source); index++ {
		destination[index] = source[index]
	}
	goto first
	goto second
}

// invariance holds the source and the destination to one value on every iteration.
func invariance() {
	var source []int
	var rows [][]int
	var held holder
	pointer := &held
	for index, value := range source {
		rows[0][index] = value
	}
	for index, value := range source {
		rows[index][index] = value
	}
	for index, value := range source {
		rows[value][index] = value
	}
	for index := range source {
		rows[index][index] = source[index]
	}
	// A call may have side effects, a conversion and a selector may not.
	for index, value := range values() {
		named(source)[index] = value
	}
	for index, value := range source {
		named(source)[index] = value
	}
	for index, value := range source {
		pointer.inner.values[index] = value
	}
	for index, value := range source {
		(*pointer).values[index] = value
	}
	// A receive has side effects.
	var channel chan []int
	for index, value := range <-channel {
		source[index] = value
	}
}

// elementTypes holds the source's and destination's element types to one.
func elementTypes() {
	var integers []int
	var others []int64
	var anything []any
	var mapped map[int]int
	var text string
	var bytes []byte
	for index, value := range integers {
		anything[index] = value
	}
	for index := range integers {
		others[index] = int64(integers[index])
	}
	for index, value := range integers {
		mapped[index] = value
	}
	for index := range text {
		bytes[index] = text[index]
	}
	var namedSlice named
	for index, value := range integers {
		namedSlice[index] = value
	}
	for index, value := range namedSlice {
		integers[index] = value
	}
	var sequence iter.Seq2[int, int]
	for index, value := range sequence {
		integers[index] = value
	}
}

// arrays are assigned whole when both are of one type, and copied through a slice of all of them otherwise.
func arrays() {
	var slice []int
	var first, second [4]int
	var longer [8]int
	var namedOne namedArray
	for index := range first {
		second[index] = first[index]
	}
	for index, value := range first {
		second[index] = value
	}
	for index := 0; index < len(first); index++ {
		second[index] = first[index]
	}
	for index := range first {
		longer[index] = first[index]
	}
	for index := range first {
		namedOne[index] = first[index]
	}
	for index := range namedOne {
		first[index] = namedOne[index]
	}
	for index := range first {
		slice[index] = first[index]
	}
	for index := range slice {
		first[index] = slice[index]
	}
	firstPointer, secondPointer, longerPointer := &first, &second, &longer
	for index := range firstPointer {
		secondPointer[index] = firstPointer[index]
	}
	for index := range firstPointer {
		longerPointer[index] = firstPointer[index]
	}
	for index := range firstPointer {
		second[index] = firstPointer[index]
	}
	for index := range first {
		secondPointer[index] = first[index]
	}
	for index := range *firstPointer {
		(*secondPointer)[index] = (*firstPointer)[index]
	}
	var held holder
	for index := range held.array {
		first[index] = held.array[index]
	}
	var namedPointed namedPointer = &first
	for index := range namedPointed {
		second[index] = namedPointed[index]
	}
	for index := range namedPointed {
		slice[index] = namedPointed[index]
	}
}

// generic sources and destinations: a type parameter's own type has no element type here, a slice of one does.
func generic[Element any, Slice ~[]Element](source []Element, constrained Slice) {
	var destination []Element
	for index, value := range source {
		destination[index] = value
	}
	for index, value := range constrained {
		destination[index] = value
	}
	for index := range source {
		constrained[index] = source[index]
	}
	var first, second [2]Element
	for index := range first {
		second[index] = first[index]
	}
}

// shadowedCopy declares a copy of its own, so the loop is no call to the builtin, and an array assignment still is.
func shadowedCopy() {
	var source, destination []int
	var first, second [4]int
	{
		copy := 1
		_ = copy
		for index, value := range source {
			destination[index] = value
		}
		for index := range first {
			second[index] = first[index]
		}
	}
	// A copy declared after the loop is not in its scope where it starts.
	for index, value := range source {
		destination[index] = value
	}
	copy := 2
	_ = copy
}

// multiline loops are replaced whole, comments and all.
func multiline() {
	var source, destination []int
	for index, value := range source { // the loop
		// stored
		destination[index] = value // here
	}
	for index := 0; index <
		len(source); index++ {
		destination[index] = source[index]
	}
}

// literals copies inside a function literal, which the walk reaches as it reaches any other node.
var literals = func() int {
	var source, destination []int
	for index, value := range source {
		destination[index] = value
	}
	return len(destination)
}
