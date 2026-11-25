// Package lookalike holds initializers that look like a restated zero and are not, none of which the rule reports.
package lookalike

// Point is a struct, whose composite literal is a value written out.
type Point struct{ X, Y int }

// A composite literal is a value, not a restated zero. For a slice or a map it is not even the zero: an empty slice
// is not nil, and a map made this way can be written to where a nil map panics.
var (
	origin Point          = Point{}
	pair   [2]int         = [2]int{}
	list   []int          = []int{}
	table  map[string]int = map[string]int{}
)

// The zero of an interface is nil, so a zero, a false or an empty string boxed in one is a value it holds.
var (
	boxedZero  any = 0
	boxedFalse any = false
	boxedEmpty any = ""
)

// A string holding a zero digit, or a rune holding a space, is not the zero.
var (
	digit string = "0"
	space rune   = ' '
)

// A local named nil or false is whatever it was set to, not the predeclared value.
func shadowed() (int, bool) {
	nil := 0
	false := true
	var count int = nil
	var done bool = false
	return count, done
}
