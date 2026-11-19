// Package forward holds a cycle of calls declared so that a walk in declaration order enters it at forward. back
// passes its slice to forward, and forward calls back and passes its own slice to pairs, which checks it, so a slice
// passed to any of the three reaches the check, and odd's is reported (#8d9r2rh). Package back, beside this one, is
// the same functions in another order. staticcheck's walk, which reads a begun function's marks as far as they have
// come, reports odd there and not here: enter takes it into forward, forward into back, and back reads forward's
// slice before forward has passed it to pairs.
package forward

// enter is declared first, so a walk in declaration order begins here and enters the cycle at forward.
func enter() {
	forward(nil)
}

// back passes its slice on to forward, so it needs it of even length as forward does.
func back(values []string) {
	forward(values)
}

// forward calls back, which closes the cycle, and passes its slice on to pairs.
func forward(values []string) {
	back(nil)
	pairs(values)
}

// pairs panics unless its values come in pairs.
func pairs(values []string) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// odd passes back a slice of one. The record is ruled here. It is true by the least fixed point, the definition
// the port marks by: back passes its slice to forward and forward to pairs, which checks it, as package back's same
// functions are reported by both. staticcheck reports it in some orders the package can declare its functions in and
// not in others, order/back's among the first and this one among the second
// (TestStaticcheckReportsACyclesFindingsInTheseOrders). At run time back calls forward, and forward calls back before
// it reaches pairs, so the two call each other without end and the stack overflows before the check could panic.
func odd() {
	back([]string{"one"}) // port finds: argument "values" is expected to have even number of elements, but has 1 elements
}
