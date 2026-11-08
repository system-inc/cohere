package parity

// ints is a named slice type, never identical to []int.
type ints []int

// holder holds slices reached by selectors.
type holder struct {
	items []int
	more  []int
}

// pureInts is pure by staticcheck's purity facts: basic parameters, a result, and no call, store or heap literal,
// and it is no stub, since making a slice is work.
func pureInts(count int) []int {
	return make([]int, count)
}

// pureTable is pure the same way, and its result is a map, so an index of it can be assigned to.
func pureTable(count int) map[int][]int {
	return make(map[int][]int, count)
}

// stubTable only returns a constant, a stub, which the purity facts never judge pure.
func stubTable(count int) map[int][]int {
	return nil
}

// impureInts allocates a literal on the heap, so it is not pure.
func impureInts() []int {
	return []int{1, 2, 3}
}

// calls counts impureTable's calls.
var calls int

// impureTable stores to a package variable, so it is not pure.
func impureTable() map[int][]int {
	calls++
	return make(map[int][]int)
}

// Each loop over values both sides report, with the same fix: a defined value, a body in blocks of one statement,
// parentheses around every operand and the callee, a map index appended to, and a ranged call whose purity does not
// matter, since a loop over values evaluates it once.
func valueLoops(source []int, destination []int, table map[int][]int) []int {
	for _, value := range source {
		destination = append(destination, value)
	}
	for _, value := range source {
		{
			{
				destination = append(destination, value)
			}
		}
	}
	for _, value := range source {
		(destination) = (append)((destination), (value))
	}
	for _, value := range source {
		table[0] = append(table[0], value)
	}
	for _, value := range impureInts() {
		destination = append(destination, value)
	}
	for _, value := range source {
		pureTable(1)[2] = append(pureTable(1)[2], value)
	}
	return destination
}

// The near misses of a loop over values neither side reports.
func valueNearMisses(source []int, destination []int, other []int, table map[int][]int, named ints) []int {
	// A key that is not blank: the value alternative wants `_`, and the index ones want no value.
	var key, element int
	for key, element = range source {
		destination = append(destination, element)
	}
	_ = key
	// The value is not what is appended.
	for _, value := range source {
		destination = append(destination, value+1)
	}
	// The first argument is not the syntax assigned to.
	for _, value := range source {
		destination = append(other, value)
	}
	// Two values appended, and a defining assignment.
	for _, value := range source {
		destination = append(destination, value, value)
	}
	for _, value := range source {
		fresh := append(destination, value)
		_ = fresh
	}
	// A body of two statements.
	for _, value := range source {
		destination = append(destination, value)
		destination = append(destination, value)
	}
	// What is appended to refers to the value.
	for _, value := range source {
		table[value] = append(table[value], value)
	}
	// What is appended to may have side effects: an impure call, and a stub's.
	for _, value := range source {
		impureTable()[0] = append(impureTable()[0], value)
	}
	for _, value := range source {
		stubTable(1)[0] = append(stubTable(1)[0], value)
	}
	// The two slices' types are not identical.
	for _, value := range named {
		destination = append(destination, value)
	}
	var anything []any
	for _, value := range source {
		anything = append(anything, value)
	}
	// A block-local append is not the universe's.
	{
		append := func(slice []int, element int) []int { return slice }
		for _, value := range source {
			destination = append(destination, value)
		}
	}
	// A map ranged over gives values, but its type is no slice's.
	for _, value := range map[string]int{} {
		destination = append(destination, value)
	}
	return destination
}

// Each loop over indexes both sides report, with the same fix: a defined key, parentheses around the element and its
// index, slices reached by a selector, a pure ranged call, and the defining alternative, the element held first.
func indexLoops(source []int, destination []int, box holder) []int {
	for position := range source {
		destination = append(destination, source[position])
	}
	for position := range source {
		destination = append(destination, (source[(position)]))
	}
	for position := range box.items {
		box.more = append(box.more, box.items[position])
	}
	for position := range pureInts(3) {
		destination = append(destination, pureInts(3)[position])
	}
	for position := range source {
		value := source[position]
		destination = append(destination, value)
	}
	for position := range source {
		value := (source[position])
		{
			destination = append(destination, (value))
		}
	}
	return destination
}

// The near misses of a loop over indexes neither side reports.
func indexNearMisses(
	source []int, destination []int, other []int, box holder, nested [][]int, table map[int][]int, text string,
) []int {
	// The element of another slice, or at another index.
	for position := range source {
		destination = append(destination, other[position])
	}
	for position := range source {
		destination = append(destination, source[position+0])
	}
	// The slice spelled another way: the pattern compares the two by syntax.
	for position := range box.items {
		destination = append(destination, holder{}.items[position])
	}
	// An impure ranged call, evaluated again on every iteration.
	for position := range impureInts() {
		destination = append(destination, impureInts()[position])
	}
	// What is appended to refers to the index.
	for position := range source {
		nested[position] = append(nested[position], source[position])
	}
	// The two types differ: an array, a pointer to one and a string.
	var array [3]int
	for position := range array {
		destination = append(destination, array[position])
	}
	for position := range &array {
		destination = append(destination, (&array)[position])
	}
	var bytes []byte
	for position := range text {
		bytes = append(bytes, text[position])
	}
	// The defining alternative's near misses: an assignment, not a definition; a value that is not appended; a body
	// of three statements.
	var held int
	for position := range source {
		held = source[position]
		destination = append(destination, held)
	}
	for position := range source {
		value := source[position]
		destination = append(destination, value+1)
	}
	for position := range source {
		value := source[position]
		destination = append(destination, value)
		destination = append(destination, value)
	}
	// What is appended to refers to the defined value.
	for position := range source {
		value := source[position]
		table[value] = append(table[value], value)
	}
	// An integer ranged over has no slice to append.
	for position := range 3 {
		destination = append(destination, source[position])
	}
	return destination
}

// A type parameter's slice is identical to itself, so both sides report each loop.
func generic[Slice ~[]Element, Element any](source Slice, destination Slice) Slice {
	for _, value := range source {
		destination = append(destination, value)
	}
	for position := range source {
		destination = append(destination, source[position])
	}
	return destination
}
