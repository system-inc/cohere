// Package parity holds the constant groups SA9004 judges, flagged and left alone, for the port and the original to
// judge side by side: groups of every kind of literal, signed literals, trailing comments, groups split by blank and
// comment lines, groups inside a function, and the groups it leaves alone.
package parity

type level int

type name string

const (
	first  level = 1
	second       = 2
	third        = 3
)

const (
	hexadecimal uint16 = 0x10
	octal              = 0o17
	negative           = -1
	positive           = +4
)

const (
	floating float64 = 1.5
	whole            = 2
	letter           = 'a'
)

const (
	greeting name = "hello" // the first
	farewell      = "bye"   // the second
)

const (
	imaginary complex128 = 1i
	another              = 2i
)

const (
	alpha int = 1
	beta      = 2

	gamma int = 3
	delta     = 4
	// A comment line splits the group as a blank one does.
	epsilon     = 5
	zeta    int = 6
	eta         = 7
)

func inside() {
	const (
		local int = 1
		other     = 2
	)
	_, _ = local, other
}

const single int = 1

const (
	untypedFirst     = 1
	typedSecond  int = 2
)

const (
	enumerated int = iota
	following
)

const (
	typedStart int = 1
	typedAgain     = 2
	typedLater int = 3
)

const (
	pairFirst, pairSecond int = 1, 2
	pairThird                 = 3
)

const (
	shifted  int = 1
	computed     = 1 << 2
)

const (
	referenced int = 1
	referring      = referenced
)

const (
	text  int = 1
	words     = "two"
)

const (
	lonely int = 1
)

const (
	iotaBased int = 1
	negated       = -iota
)
