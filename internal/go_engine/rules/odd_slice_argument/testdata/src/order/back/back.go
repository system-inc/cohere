// Package back is package forward's functions declared in another order, so that a walk in declaration order enters
// the cycle at back, not at forward. Each function reads as it does there, a slice passed to back, forward or pairs
// reaches the check as it does there, and odd's is reported as it is there (#8d9r2rh). staticcheck's walk, which
// reads a begun function's marks as far as they have come, reports odd here: back takes it into forward, which
// passes its slice to pairs and is marked before back reads it.
package back

// back is declared first, so a walk in declaration order begins here. It passes its slice on to forward, so it needs
// it of even length as forward does.
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

// enter calls forward as it does in package forward, declared here after the cycle, where it enters nothing.
func enter() {
	forward(nil)
}

// odd passes back a slice of one.
func odd() {
	back([]string{"one"}) //@ diag(`argument "values" is expected to have even number of elements, but has 1 elements`)
}
