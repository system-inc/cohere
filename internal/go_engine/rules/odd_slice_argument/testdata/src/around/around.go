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

// odd passes each a slice of one. Every call here recurses without end when the program runs: each function of a
// cycle calls on around it before it reaches pairs, so the stack overflows before any length check could panic, and
// the findings hold by the definition rather than by a panic a run would meet. Both report the slice passed as first's
// left, which first passes to pairs itself, and staticcheck does so in every order the package can declare its
// functions in (TestStaticcheckReportsACyclesFindingsInTheseOrders). Each record below gives its own reason.
func odd() {
	first([]string{"one"}, nil)
	// True by the least fixed point, the definition the port marks by: swapped passes its right as the next call's
	// left, which it passes to pairs. staticcheck reports it in no order, its one visit of swapped reading the call
	// before the mark is made. At run time swapped calls itself before it reaches pairs, and never returns.
	swapped(nil, []string{"one"}) // port finds: argument "right" is expected to have even number of elements, but has 1 elements
	// True by the least fixed point: first passes its right to second as second's left, which second passes back to
	// first as first's left, which first passes to pairs, a mark gone twice around. staticcheck reports it in no order.
	// At run time first calls second, and second first, before either reaches pairs, and neither returns.
	first(nil, []string{"one"}) // port finds: argument "right" is expected to have even number of elements, but has 1 elements
	// True by the least fixed point: second passes its right to first as first's right, marked as the record above
	// says. staticcheck reports it in no order. At run time second calls first before anything reaches pairs, and the
	// two call each other without end.
	second(nil, []string{"one"}) // port finds: argument "right" is expected to have even number of elements, but has 1 elements
}
