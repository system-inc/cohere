// Package cycle holds two functions that pass a slice to each other, the place the push's marks hang on the order it
// visits the package in. Visited as declared, Outer begins first; Inner, calling back into it, reads Outer's marks as
// far as they have come, none, then passes its slice to Pairs and is marked; Outer, reading Inner's mark, is marked
// too. Begun at Inner, as a reader asking of Inner alone would begin, Outer would read Inner unmarked and stay so.
package cycle

// Pairs needs an even number of strings.
func Pairs(pairs []string) {
	if len(pairs)%2 != 0 {
		panic("pairs has odd length")
	}
}

// Outer is marked by the push, which visits it first.
func Outer(values []string) {
	Inner(values)
}

// Inner is marked through Pairs.
func Inner(values []string) {
	Outer(values)
	Pairs(values)
}
