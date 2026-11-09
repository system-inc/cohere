package differences

// Pointers to array literals and to new arrays, each written at an index that nothing reads. Both find each write, in
// other words: the port names an index as its source writes it, and x/tools as its single assignment form holds it, a
// constant as value and type, 0:int, so each is recorded as a rename. An element a literal gives by position has no
// index in its source, so the port names it by its value alone.

const zeroIndex int8 = 0

type single [1]int

// A pointer to an array literal of one element, its length read. The port names the element's index by its value, 0,
// since the source gives it by position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteral() int {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	return len(values)
}

// The same, keyed. The port names the index as its key is written, 0, where the original prints 0:int.
func pointerKeyedArrayLiteral() int {
	values := &[1]int{0: 7} // port renames: unused write to array index 0:int => unused write to array index 0
	return len(values)
}

// The same, keyed by a typed constant, which the original names by its value and type, 0:int8, and the port as the key
// is written, zeroIndex.
func pointerTypedKeyArrayLiteral() int {
	values := &[1]int{zeroIndex: 7} // port renames: unused write to array index 0:int8 => unused write to array index zeroIndex
	return len(values)
}

// The same, its capacity read. The port names the element's index by its value, 0, since the source gives it by
// position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralCapacity() int {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	return cap(values)
}

// The same, declared with var. The port names the element's index by its value, 0, since the source gives it by
// position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralDeclared() int {
	var values = &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	return len(values)
}

// The same, of a named array type. The port names the element's index by its value, 0, since the source gives it by
// position and writes none, where the original prints value and type, 0:int.
func pointerNamedArrayLiteral() int {
	values := &single{7} // port renames: unused write to array index 0:int => unused write to array index 0
	return len(values)
}

// The same, ranged over by index alone. The port names the element's index by its value, 0, since the source gives it
// by position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralRanged() int {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	total := 0
	for index := range values {
		total += index
	}
	return total
}

// The same, ranged over with a blank value. The port names the element's index by its value, 0, since the source gives
// it by position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralRangedBlank() int {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	total := 0
	for index, _ := range values {
		total += index
	}
	return total
}

// The same, a whole array stored through the pointer. The port names the element's index by its value, 0, since the
// source gives it by position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralStoredWhole() int {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	*values = [1]int{8}
	return cap(values)
}

// The same, the variable then given another pointer. The port names the element's index by its value, 0, since the
// source gives it by position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralReassigned(other *[1]int) int {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	values = other
	return len(values)
}

// The same, a whole array stored through it before it is given another pointer. The port names the element's index by
// its value, 0, since the source gives it by position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralStoredThenReassigned(other *[1]int) int {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	*values = [1]int{9}
	values = other
	return len(values)
}

// The pointer copied into another variable, whose length is read. The port names the element's index by its value, 0,
// since the source gives it by position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralCopied() int {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	other := values
	return len(other)
}

// Copied twice. The port names the element's index by its value, 0, since the source gives it by position and writes
// none, where the original prints value and type, 0:int.
func pointerArrayLiteralCopiedTwice() int {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	other := values
	third := other
	return len(third)
}

// Its length read by a closure, which captures nothing. The port names the element's index by its value, 0, since the
// source gives it by position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralLengthInClosure() func() int {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	return func() int { return len(values) }
}

// Assigned to the blank identifier. The port names the element's index by its value, 0, since the source gives it by
// position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralToBlank() {
	_ = &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
}

// Its variable assigned to the blank identifier. The port names the element's index by its value, 0, since the source
// gives it by position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralVariableToBlank() {
	values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
	_ = values
}

// In the body of a loop over an iterator function, a function of its own. The port names the element's index by its
// value, 0, since the source gives it by position and writes none, where the original prints value and type, 0:int.
func pointerArrayLiteralInIterator(sequence func(func(int) bool)) {
	for range sequence {
		values := &[1]int{7} // port renames: unused write to array index 0:int => unused write to array index 0
		_ = len(values)
	}
}

// An element written through a pointer to an empty array literal, which both forms keep. The port names the index as
// written, 0, where the original prints 0:int.
func pointerEmptyArrayLiteralWritten() int {
	values := &[1]int{}
	values[0] = 5 // port renames: unused write to array index 0:int => unused write to array index 0
	return len(values)
}

// The same, of two elements, its capacity read. The port names the index as written, 1, where the original prints
// 1:int.
func pointerEmptyArrayLiteralOfTwoWritten() int {
	values := &[2]int{}
	values[1] = 5 // port renames: unused write to array index 1:int => unused write to array index 1
	return cap(values)
}

// The same, of an array from new. The port names the index as written, 0, where the original prints 0:int.
func newArrayWritten() int {
	values := new([1]int)
	values[0] = 5 // port renames: unused write to array index 0:int => unused write to array index 0
	return len(values)
}

// The same, ranged over by index. The port names the index as written, 1, where the original prints 1:int.
func pointerEmptyArrayLiteralWrittenThenRanged() int {
	values := &[2]int{}
	values[1] = 5 // port renames: unused write to array index 1:int => unused write to array index 1
	total := 0
	for index := range values {
		total += index
	}
	return total
}

// The same, assigned to the blank identifier. The port names the index as written, 0, where the original prints 0:int.
func pointerEmptyArrayLiteralWrittenToBlank() {
	values := &[1]int{}
	values[0] = 5 // port renames: unused write to array index 0:int => unused write to array index 0
	_ = values
}
