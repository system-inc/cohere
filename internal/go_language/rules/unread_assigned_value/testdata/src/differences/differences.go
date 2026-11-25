// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none. The original follows a value through phis with one set of the phis it has seen,
// shared across the whole walk, and reads the switch tags of the outer function's syntax, nested literals included,
// so these values pass through loops of phis that reach each other and switches nested in closures, where a walk of
// the port's own would show as a finding one side makes and the other does not.
package differences

func use(values ...any) {}

func produce() int { return 1 }

// Found by both, four times: two variables swapped in a loop, each phi reaching the other, neither read after.
func swapped(rows int) {
	first, second := produce(), produce()
	for row := 0; row < rows; row++ {
		first, second = second, first
	}
}

// Found by neither: the same swap, one read after the loop.
func swappedRead(rows int) {
	first, second := produce(), produce()
	for row := 0; row < rows; row++ {
		first, second = second, first
	}
	use(first)
}

// Found by both: a switch on another value in a closure reads nothing of the outer function's.
func switchInClosure() {
	value := produce()
	value = produce()
	use(func() {
		switch produce() {
		}
	}, value)
}
