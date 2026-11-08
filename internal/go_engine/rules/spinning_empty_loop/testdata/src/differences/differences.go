// Package differences holds the loops where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: a `false` the code declares, a spared constant of a type the code declares, a
// constant tested against a value, and conditions whose only call is a builtin or a conversion.
package differences

type state bool

const stopped state = false

func shadowed(flag bool) {
	false := flag
	for false {
	}
}

func plausible(values []int, flag bool) {
	for stopped {
	}
	for state(flag) {
	}
	for cap(values) > 0 {
	}
	for values == nil {
	}
	for flag == true {
	}
	for stopped == false {
	}
}
