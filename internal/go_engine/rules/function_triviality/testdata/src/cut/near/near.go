// Package near calls far's trivial function, whose body a reader of near never holds.
package near

import "cut/far"

// Crossing calls leaf and then far.Nothing: not trivial to a reader of near, which holds far by its types alone.
func Crossing() error {
	leaf()
	far.Nothing()
	return last()
}

func leaf() {}

func last() error { return nil }
