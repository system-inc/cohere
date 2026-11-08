package differences

// nested is a slice of itself, so appending an element spread, `append(target, element...)`, type-checks, and
// appends what the element holds, where `append(target, source...)` would append the elements themselves.
type nested []nested

// The original ignores the spread, as the pattern language's CallExpr has no field for it, and suggests a loop that
// flattens one level be replaced with one that does not. The port drops each, in all three alternatives.
func spreads(source nested, target nested) nested {
	// The original's fix appends each element of source, where the loop appends what each element holds.
	for _, element := range source { // port drops: should replace loop with target = append(target, source...)
		target = append(target, element...)
	}
	// The same, through the element at each index.
	for index := range source { // port drops: should replace loop with target = append(target, source...)
		target = append(target, source[index]...)
	}
	// The same, through the element held first.
	for index := range source { // port drops: should replace loop with target = append(target, source...)
		element := source[index]
		target = append(target, element...)
	}
	// Without the spread, the loop appends each element itself, which the one append does, so both report it.
	for _, element := range source {
		target = append(target, element)
	}
	return target
}

// A loop that assigns its key or value with = to a variable declared outside the for statement leaves the last
// element or index in it, and the original's fix, one append, leaves the variable as it was. Both report each loop,
// and the port withholds the fix, judging by where the variable is declared, not by whether anything reads it after.
func outerVariables(source []int, target []int, parameter int) ([]int, int) {
	// Read after the loop: the fix would return 0 here, where the loop returns the last element.
	var last int
	for _, last = range source { // port withholds: Replace loop with call to append
		target = append(target, last)
	}
	// Not read after the loop, and withheld all the same, since the port asks no question of flow.
	var position int
	for position = range source { // port withholds: Replace loop with call to append
		target = append(target, source[position])
	}
	// A parameter is declared outside the for statement too, and the third alternative's key the same way.
	for parameter = range source { // port withholds: Replace loop with call to append
		element := source[parameter]
		target = append(target, element)
	}
	// The same loop with its key defined by the header keeps its fix, as the original's, since the key ends with it.
	for index := range source {
		element := source[index]
		target = append(target, element)
	}
	return target, last
}

// The shapes where the two could most plausibly part, and agree: a value variable shadowed inside the body, which is
// another object; a slice ranged over that the body appends to as well; a value held in a variable named like the
// slice it is read from; and a loop variable of a function literal.
func agreements(source []int, target []int) []int {
	// The value is shadowed by a block of two statements, which no alternative matches.
	for _, value := range source {
		{
			value := value + 1
			target = append(target, value)
		}
	}
	// Appending to the slice ranged over evaluates it once, as the one append does.
	for _, value := range target {
		target = append(target, value)
	}
	for index := range target {
		target = append(target, target[index])
	}
	// The element is held in a variable spelled like the slice, read before the variable is defined.
	for index := range source {
		source := source[index]
		target = append(target, source)
	}
	// A loop in a function literal.
	appendAll := func(from []int, to []int) []int {
		for _, value := range from {
			to = append(to, value)
		}
		return to
	}
	return appendAll(source, target)
}
