// Package around holds cycles of calls a mark must go around more than once, which staticcheck's walk, visiting each
// function once, misses in every order a package can declare them in (#8d9r2rh). The port marks the least set, in
// which a mark goes around as often as the passes carry it.
package around

// pairs panics unless its values come in pairs.
func pairs(values []string) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// swapped calls itself with its slices swapped, then passes left to pairs. Its left needs an even length by pairs, and
// its right by being passed as the left of the next call. staticcheck reads the first call while swapped has no mark
// yet, and never reads it again.
func swapped(left, right []string) {
	swapped(right, left)
	pairs(left)
}

// first passes its slices to second, swapped, then its left to pairs, and second passes its own back to first as they
// are. first's left needs an even length by pairs, second's left by first's left, first's right by second's left, and
// second's right by first's right: the mark goes twice around. Entered at either, staticcheck leaves both rights
// unmarked, each read before the other's walk has ended.
func first(left, right []string) {
	second(right, left)
	pairs(left)
}

// second passes its slices on to first as they are.
func second(left, right []string) {
	first(left, right)
}

// odd passes each a slice of one. The records are ruled here: the port rightly reports each slice passed as a right,
// which reaches pairs's check as a left one call on, where staticcheck's one visit of each function has read the
// cycle before the mark was made. Both report the slice passed as first's left, which first passes to pairs itself.
func odd() {
	first([]string{"one"}, nil)
	swapped(nil, []string{"one"}) // port finds: argument "right" is expected to have even number of elements, but has 1 elements
	first(nil, []string{"one"})   // port finds: argument "right" is expected to have even number of elements, but has 1 elements
	second(nil, []string{"one"})  // port finds: argument "right" is expected to have even number of elements, but has 1 elements
}
