// Package parity holds the range statements S1029 judges, each with a comment saying whether staticcheck's S1029 is
// expected to report it: the index dropped or read, what the string and the slice are typed as, what else reads the
// slice, and where the range sits. generated.go beside it holds one in a generated file, which the original does not
// filter, so both report it.
package parity

type label string

type runes []rune

type character = rune

// Reported: the plain conversion, its result held first, a named string, int32 spelled out, an alias of rune, a
// named slice of runes, a constant and a range assigning to a variable declared before it.
func reported(text string, named label) {
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
	for _, r := range []rune("héllo") {
		println(r)
	}
	var r rune
	for _, r = range []rune(text) {
		println(r)
	}
}

// Not reported: the index read, no index at all, the blank index with no value, the string itself, a slice of bytes,
// a slicing of the conversion, the slice read again, the slice ranged over twice and a slice of runes not converted.
func notReported(text string, given []rune) {
	for index, r := range []rune(text) {
		println(index, r)
	}
	for range []rune(text) {
		println()
	}
	for _ = range []rune(text) {
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
	for _, r := range given {
		println(r)
	}
}

// Reported: in a function literal and in a goroutine.
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
}

type holder struct{ text string }

// Reported: in a method.
func (value holder) method() {
	for _, r := range []rune(value.text) {
		println(r)
	}
}

// Reported: a type parameter constrained to strings converted to a type parameter constrained to slices of runes.
// Not reported: a type parameter that may be a string or a slice of runes has no core type, so no string's.
func generic[Text ~string, Runes ~[]rune, Either string | []rune](text Text, either Either) {
	for _, r := range Runes(text) {
		println(r)
	}
	for _, r := range []rune(either) {
		println(r)
	}
}
