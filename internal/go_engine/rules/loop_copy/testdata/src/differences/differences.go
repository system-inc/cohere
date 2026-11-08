// Package differences holds the shapes where the port and the original could most plausibly part, and records no
// finding of either alone, since the two agree on each.
package differences

// literalDestinations index a composite literal, whose function literal holds identifiers with no object: a type
// switch's symbol and a blank. With a value, no identifier with no object is the loop's; without one, the original
// compares each with the nil value it holds, so they count as the loop's and the loop is left alone.
func literalDestinations() {
	var functions []func()
	for index, function := range functions {
		[]func(){func() {
			switch symbol := any(0).(type) {
			default:
				_ = symbol
			}
		}}[index] = function
	}
	for index := range functions {
		[]func(){func() {
			switch symbol := any(0).(type) {
			default:
				_ = symbol
			}
		}}[index] = functions[index]
	}
}

// dereferenced sources and destinations print in the fixes with the parentheses the printer needs, not the ones the
// source held, which the pattern removed before it bound them.
func dereferenced() {
	var first, second [4]int
	var slice []int
	firstPointer, secondPointer := &first, &second
	pointers := &firstPointer
	for index := range *firstPointer {
		(*secondPointer)[index] = (*firstPointer)[index]
	}
	for index := range *firstPointer {
		slice[index] = (*firstPointer)[index]
	}
	for index := range slice {
		(**pointers)[index] = slice[index]
	}
	for index := range *pointers {
		second[index] = (*pointers)[index]
	}
	slices := &[][]int{}
	for index, value := range slice {
		(*slices)[0][index] = value
	}
}

// repeated sources are the same by syntax, not by value: a source that differs only in parentheses is one, and a call
// ranged over and called again is the same syntax, which the side effects of the call then refuse.
func repeated() {
	var destination []int
	var rows [][]int
	for index := range rows[0] {
		destination[index] = rows[(0)][index]
	}
	for index := range rows[0] {
		destination[index] = rows[1][index]
	}
	for index := range produce() {
		destination[index] = produce()[index]
	}
	for index := 0; index < len(produce()); index++ {
		destination[index] = produce()[index]
	}
}

func produce() []int { return nil }

// shadowedKey stores at a key spelled like the loop's, which by syntax is the loop's, and is through the body's one
// statement, which declares nothing to shadow it.
func shadowedKey() {
	var source, destination []int
	index := 0
	for index := range source {
		destination[index] = source[index]
	}
	_ = index
}
