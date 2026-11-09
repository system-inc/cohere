package differences

// Arrays written at an index that nothing reads, by an index expression or by a key of a literal. Both find each
// write, in other words: the port names an index as its source writes it, and x/tools as its single assignment form
// holds it, a constant as value and type and an index the function computes by its registers, so each is recorded as a
// rename.

// An array written whole and then by element. The port names the index as written, 1, where the original prints 1:int.
func wholeThenElement() {
	values := [2]int{}
	values = [2]int{3, 4}
	values[1] = 5 // port renames: unused write to array index 1:int => unused write to array index 1
}

// A single element written at a parameter's index. The port names the index as written, index, where the original names
// the parameter, parameter index : int.
func parameterIndex(index int) {
	var values [4]int
	values[index] = 1 // port renames: unused write to array index parameter index : int => unused write to array index index
}

// A single element written at a computed index. The port names the index as written, index + 1, printed with gofmt's
// spacing, where the original prints index + 1:int.
func computedIndexAlone(index int) {
	var values [4]int
	values[index+1] = 1 // port renames: unused write to array index index + 1:int => unused write to array index index + 1
}

// A pointer to a partial array literal, keyed. The port names the index as its key is written, 2, where the original
// prints 2:int.
func pointerPartialArrayLiteral() int {
	values := &[3]int{2: 7} // port renames: unused write to array index 2:int => unused write to array index 2
	return len(values)
}

// A partial array literal into a variable, then an element written. The port names the index as written, 2, where the
// original prints 2:int.
func partialArrayThenElement() {
	values := [3]int{1: 5}
	values[2] = 1 // port renames: unused write to array index 2:int => unused write to array index 2
}
