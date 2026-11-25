// Package home holds two cycles of calls, each passing a slice across a package to other.G as well as on to pairs,
// which checks it, so a walk of either cycle meets other.G's parameter in another package (#8d9r2rh). Every slice of
// both cycles is marked by pairs's check, whichever function a question enters the cycle at, and the walk must ask
// other.G's parameter, and so read it, wherever it enters, or the package's reads would turn on the order of its
// questions.
package home

import "entry/other"

// h passes its slice to f, closing the cycle, and to pairs.
func h(values []string) {
	f(values)
	pairs(values)
}

// f passes its slice to h and to other.G.
func f(values []string) {
	h(values)
	other.G(values)
}

// a passes its slice to b and to other.G.
func a(values []string) {
	b(values)
	other.G(values)
}

// b passes its slice to c.
func b(values []string) {
	c(values)
}

// c passes its slice to a, closing the cycle of three, and to pairs.
func c(values []string) {
	a(values)
	pairs(values)
}

// pairs panics unless its values come in pairs.
func pairs(values []string) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}
