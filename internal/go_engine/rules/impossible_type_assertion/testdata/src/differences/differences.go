// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same method sets, the port through the function's own program where
// the original goes through buildir's result. The original builds its message in the order of the asserted
// interface's methods and gives up the whole assertion at the first generic method, after contradictions already
// found, so these assertions have many methods, contradictions before and after a generic one, where a judgment that
// hung on the order would show here as a finding or a message one side makes and the other does not.
package differences

type generic[Value any] interface {
	Middle() Value
}

type first interface {
	Alpha() int
	Beta() int
	Gamma() int
	Delta() int
}

type second interface {
	Delta() string
	Alpha() string
	Gamma() int
	Beta() string
}

type mixed interface {
	generic[int]
	Beta() int
	Zeta() int
}

type contradictsMixed interface {
	Beta() string
	Middle() int
	Zeta() string
}

// Found by both: three contradictions, written in the order of the asserted interface's methods.
func manyContradictions(value first) {
	_ = value.(second)
}

// Found by neither: a contradiction before the generic method, Beta, and one after it, Zeta, give the whole
// assertion up.
func contradictionsAroundGeneric(value mixed) {
	_ = value.(contradictsMixed)
}

// Found by both, once: the same interfaces asserted the other way, then the value's own interface, which agrees.
func bothWays(value second) {
	_ = value.(first)
	_ = value.(second)
}
