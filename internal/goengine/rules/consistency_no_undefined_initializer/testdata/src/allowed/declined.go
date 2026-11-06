package allowed

import "time"

// Level is a set of named constants whose first is zero.
type Level int

const (
	LevelDebug Level = iota
	LevelInformation
)

// A named constant is a choice the writer made, the first member of a set here, and says more than the zero it
// happens to equal, so it is left alone.
var level Level = LevelDebug

// A conversion, a negation and a constant expression are zero, and each is a spelling the writer chose over a bare
// literal. The rule reads only a literal, false or nil, and would rather miss these than argue with the choice.
var (
	timeout  time.Duration = time.Duration(0)
	negated  int           = -0
	balanced int           = 1 - 1
	nothing  *time.Time    = (*time.Time)(nil)
)

// Only one of these values is zero, and dropping it would mean splitting the spec, a rewrite rather than a removal.
var origin, offset int = 0, 4

// A type parameter's zero is 0 here only because every type in its constraint's type set is a number, and the rule
// does not read type sets, so a literal zero for a type parameter is left alone.
func zeroOf[Number ~int | ~float64]() Number {
	var value Number = 0
	return value
}
