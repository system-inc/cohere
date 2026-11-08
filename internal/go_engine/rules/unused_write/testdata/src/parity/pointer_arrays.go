package parity

// Pointers to array literals and to new arrays. staticcheck stores a literal that gives every element whole, and lifts
// the allocation away when nothing needs its address, where x/tools writes each element through an index address, so
// these shapes test both the port's judgment of the element staticcheck keeps and its reading of the syntax where
// staticcheck keeps nothing.

const zeroIndex int8 = 0

type single [1]int

// A pointer to an array literal of one element, its length read.
func pointerArrayLiteral() int {
	values := &[1]int{7}
	return len(values)
}

// The same, keyed.
func pointerKeyedArrayLiteral() int {
	values := &[1]int{0: 7}
	return len(values)
}

// The same, keyed by a typed constant, which names the index's type.
func pointerTypedKeyArrayLiteral() int {
	values := &[1]int{zeroIndex: 7}
	return len(values)
}

// The same, its capacity read.
func pointerArrayLiteralCapacity() int {
	values := &[1]int{7}
	return cap(values)
}

// The same, declared with var.
func pointerArrayLiteralDeclared() int {
	var values = &[1]int{7}
	return len(values)
}

// The same, of a named array type.
func pointerNamedArrayLiteral() int {
	values := &single{7}
	return len(values)
}

// The same, ranged over by index alone.
func pointerArrayLiteralRanged() int {
	values := &[1]int{7}
	total := 0
	for index := range values {
		total += index
	}
	return total
}

// The same, ranged over with a blank value.
func pointerArrayLiteralRangedBlank() int {
	values := &[1]int{7}
	total := 0
	for index, _ := range values {
		total += index
	}
	return total
}

// The same, ranged over by value, which reads the element.
func pointerArrayLiteralRangedValue() int {
	values := &[1]int{7}
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

// The same, a whole array stored through the pointer.
func pointerArrayLiteralStoredWhole() int {
	values := &[1]int{7}
	*values = [1]int{8}
	return cap(values)
}

// The same, the variable then given another pointer.
func pointerArrayLiteralReassigned(other *[1]int) int {
	values := &[1]int{7}
	values = other
	return len(values)
}

// The same, a whole array stored through it before it is given another pointer.
func pointerArrayLiteralStoredThenReassigned(other *[1]int) int {
	values := &[1]int{7}
	*values = [1]int{9}
	values = other
	return len(values)
}

// A store through a pointer that is the literal's on one path and another's on the other.
func pointerArrayLiteralMerged(condition bool, other *[1]int) int {
	values := &[1]int{7}
	if condition {
		values = other
	}
	*values = [1]int{9}
	return len(values)
}

// The pointer copied into another variable, whose length is read.
func pointerArrayLiteralCopied() int {
	values := &[1]int{7}
	other := values
	return len(other)
}

// Copied twice.
func pointerArrayLiteralCopiedTwice() int {
	values := &[1]int{7}
	other := values
	third := other
	return len(third)
}

// Copied, and the copy read.
func pointerArrayLiteralCopiedAndRead() [1]int {
	values := &[1]int{7}
	other := values
	return *other
}

// Its length read by a closure, which captures nothing.
func pointerArrayLiteralLengthInClosure() func() int {
	values := &[1]int{7}
	return func() int { return len(values) }
}

// Assigned to the blank identifier.
func pointerArrayLiteralToBlank() {
	_ = &[1]int{7}
}

// Its variable assigned to the blank identifier.
func pointerArrayLiteralVariableToBlank() {
	values := &[1]int{7}
	_ = values
}

// Read whole.
func pointerArrayLiteralReadWhole() [1]int {
	values := &[1]int{7}
	return *values
}

// Its element read.
func pointerArrayLiteralElementRead() int {
	values := &[1]int{7}
	return values[0]
}

// Passed to a call.
func pointerArrayLiteralPassed(sink func(*[1]int)) {
	values := &[1]int{7}
	sink(values)
}

// Compared with nil.
func pointerArrayLiteralComparedWithNil() bool {
	values := &[1]int{7}
	return values == nil
}

// A literal of two elements, each element's address a use of the other's write.
func pointerArrayLiteralOfTwo() int {
	values := &[2]int{7, 8}
	return len(values)
}

// In the body of a loop over an iterator function, a function of its own.
func pointerArrayLiteralInIterator(sequence func(func(int) bool)) {
	for range sequence {
		values := &[1]int{7}
		_ = len(values)
	}
}

// An element written through a pointer to an empty array literal, which both forms keep.
func pointerEmptyArrayLiteralWritten() int {
	values := &[1]int{}
	values[0] = 5
	return len(values)
}

// The same, of two elements, its capacity read.
func pointerEmptyArrayLiteralOfTwoWritten() int {
	values := &[2]int{}
	values[1] = 5
	return cap(values)
}

// The same, of an array from new.
func newArrayWritten() int {
	values := new([1]int)
	values[0] = 5
	return len(values)
}

// The same, ranged over by index.
func pointerEmptyArrayLiteralWrittenThenRanged() int {
	values := &[2]int{}
	values[1] = 5
	total := 0
	for index := range values {
		total += index
	}
	return total
}

// The same, assigned to the blank identifier.
func pointerEmptyArrayLiteralWrittenToBlank() {
	values := &[1]int{}
	values[0] = 5
	_ = values
}
