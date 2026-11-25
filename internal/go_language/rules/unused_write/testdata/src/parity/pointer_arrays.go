package parity

// Pointers to array literals and to new arrays. staticcheck stores a literal that gives every element whole, and lifts
// the allocation away when nothing needs its address, where x/tools writes each element through an index address, so
// these shapes test both the port's judgment of the element staticcheck keeps and its reading of the syntax where
// staticcheck keeps nothing.

// A pointer to an array literal of one element, ranged over by value, which reads the element.
func pointerArrayLiteralRangedValue() int {
	values := &[1]int{7}
	total := 0
	for _, value := range values {
		total += value
	}
	return total
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

// A pointer to an array literal of one element copied into another variable, and the copy read.
func pointerArrayLiteralCopiedAndRead() [1]int {
	values := &[1]int{7}
	other := values
	return *other
}

// A pointer to an array literal of one element, read whole.
func pointerArrayLiteralReadWhole() [1]int {
	values := &[1]int{7}
	return *values
}

// A pointer to an array literal of one element, its element read.
func pointerArrayLiteralElementRead() int {
	values := &[1]int{7}
	return values[0]
}

// A pointer to an array literal of one element, passed to a call.
func pointerArrayLiteralPassed(sink func(*[1]int)) {
	values := &[1]int{7}
	sink(values)
}

// A pointer to an array literal of one element, compared with nil.
func pointerArrayLiteralComparedWithNil() bool {
	values := &[1]int{7}
	return values == nil
}

// A literal of two elements, each element's address a use of the other's write.
func pointerArrayLiteralOfTwo() int {
	values := &[2]int{7, 8}
	return len(values)
}
