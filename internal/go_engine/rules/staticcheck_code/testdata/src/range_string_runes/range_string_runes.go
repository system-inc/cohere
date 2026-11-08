// Package range_string_runes holds the range statements RangeStringRunes judges, each with a comment saying whether
// staticcheck's sharedcheck.CheckRangeStringRunes is expected to report it: what is ranged over, how the conversion is
// spelled and typed, what else reads the slice, and where the range sits, in closures, goroutines and generic code.
package range_string_runes

type name string

type runes []rune

type character = rune

type record struct{ label string }

// Reported: the plain conversion, its result held in a variable, a named string, int32 spelled out, an alias of rune,
// a named slice of runes, a parenthesized type and a range assigning to a variable declared before it. gofmt drops
// parentheses around a range's operand, so the original's unwrapping of them is met by no formatted file.
func reported(text string, named name, value record) {
	for _, r := range []rune(text) {
		println(r)
	}
	held := []rune(text)
	for _, r := range held {
		println(r)
	}
	for _, r := range []rune(named) {
		println(r)
	}
	for _, r := range []int32(text) {
		println(r)
	}
	for _, r := range []character(text) {
		println(r)
	}
	for _, r := range runes(text) {
		println(r)
	}
	for _, r := range ([]rune)(value.label) {
		println(r)
	}
	var r rune
	for _, r = range []rune(text) {
		println(r)
	}
}

// Not reported: the index read, no index at all, ranging over the string, a slice of bytes, a slicing of the
// conversion, the slice read again or ranged over twice.
func notReported(text string) {
	for index, r := range []rune(text) {
		println(index, r)
	}
	for range []rune(text) {
		println()
	}
	for _, r := range text {
		println(r)
	}
	for _, b := range []byte(text) {
		println(b)
	}
	for _, r := range []rune(text)[1:] {
		println(r)
	}
	again := []rune(text)
	for _, r := range again {
		println(r)
	}
	println(len(again))
	twice := []rune(text)
	for _, r := range twice {
		println(r)
	}
	for _, r := range twice {
		println(r)
	}
}

// Not reported: the blank index with no value, whose elements are never read, so the slice has one reader alone.
// Reported: a constant string, a literal and a named constant.
func blankAndConstant() {
	for _ = range []rune("text") {
		println()
	}
	for _, r := range []rune("héllo") {
		println(r)
	}
	const greeting = "hello"
	for _, r := range []rune(greeting) {
		println(r)
	}
}

// Reported once each: in a literal called in place, in a goroutine, in a deferred literal and in a literal within a
// literal.
func inLiterals(text string) {
	func() {
		for _, r := range []rune(text) {
			println(r)
		}
	}()
	go func() {
		for _, r := range []rune(text) {
			println(r)
		}
	}()
	defer func() {
		inner := func() {
			for _, r := range []rune(text) {
				println(r)
			}
		}
		inner()
	}()
}

// Reported: a type parameter constrained to strings, converted to a slice of runes and to a type parameter
// constrained to slices of runes.
func generic[Text ~string, Runes ~[]rune](text Text) {
	for _, r := range []rune(text) {
		println(r)
	}
	for _, r := range Runes(text) {
		println(r)
	}
}

// Not reported: a type parameter that may be a string or a slice of bytes has no core type of a string.
func genericUnion[Text string | []byte](text Text) {
	for _, r := range string(text) {
		println(r)
	}
}

type holder struct{ text string }

// Reported: in a method.
func (value holder) method() {
	for _, r := range []rune(value.text) {
		println(r)
	}
}

// Reported: in a package-level variable's function literal.
var initialized = func(text string) int {
	count := 0
	for _, r := range []rune(text) {
		count += int(r)
	}
	return count
}
