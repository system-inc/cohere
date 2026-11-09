package shapes

import (
	"fixture/alpha"
	"fixture/beta"
)

var sink []int

func record(value int) { sink = append(sink, value) }
func note(value int)   { sink = append(sink, -value) }

// Same: names only.
func SameNamesA(x, y int) int          { sum := x*2 + y; return sum }
func SameNamesB(first, second int) int { total := first*2 + second; return total }

// Same: order of two independent assignments.
func SameOrderA(x, y int) int { a := x * 3; b := y - 7; return a / b }
func SameOrderB(x, y int) int { b := y - 7; a := x * 3; return a / b }

// Same: two independent loads, swapped.
func SameLoadOrderA(p, q *int) int { a := *p; b := *q; return a - b }
func SameLoadOrderB(p, q *int) int { b := *q; a := *p; return a - b }

// Same: a + b against b + a for ints.
func SameCommuteA(a, b int) int { return (a + b) << 1 }
func SameCommuteB(a, b int) int { return (b + a) << 1 }

// Same: 16 against 0x10.
func SameLiteralA(x int) int { return x * 16 }
func SameLiteralB(x int) int { return x * 0x10 }

// Same: early return against else.
func SameEarlyA(x int) int {
	if x > 0 {
		return x
	}
	return -x
}
func SameEarlyB(x int) int {
	if x > 0 {
		return x
	} else {
		return -x
	}
}

// Different: a - b against b - a.
func DiffSubtractA(a, b int) int { return (a - b) << 2 }
func DiffSubtractB(a, b int) int { return (b - a) << 2 }

// Different: two calls in swapped order.
func DiffCallsA(x int) { record(x); note(x) }
func DiffCallsB(x int) { note(x); record(x) }

// Different: int addition against float64 addition.
func DiffTypesA(a, b int) int         { return (a + b) * 3 }
func DiffTypesB(a, b float64) float64 { return (a + b) * 3 }

// Different: same body, same-named callee in two packages.
func DiffPackagesA(x int) int { return alpha.Name(x) * 5 }
func DiffPackagesB(x int) int { return beta.Name(x) * 5 }

// Reported either way: a method on two receiver types doing the same thing.
type Box struct{ value int }
type Crate struct{ value int }

func (box Box) Doubled() int     { return box.value*2 + 9 }
func (crate Crate) Doubled() int { return crate.value*2 + 9 }

// Extra different: string concatenation is not commutative.
func DiffConcatA(a, b string) string { return a + b + "!" }
func DiffConcatB(a, b string) string { return b + a + "!" }

// Extra different: two loop-carried values, subtracted either way.
func DiffLoopA(n int) int {
	x, y := 1, 2
	for i := 0; i < n; i++ {
		x, y = x+y, x*3
	}
	return x - y
}
func DiffLoopB(n int) int {
	x, y := 1, 2
	for i := 0; i < n; i++ {
		x, y = x+y, x*3
	}
	return y - x
}

// Extra different: a load before a store against the load after it.
func DiffMemoryA(p *int) int { old := *p; *p = 4; return old + 11 }
func DiffMemoryB(p *int) int { *p = 4; old := *p; return old + 11 }

// Extra same: !c with swapped arms.
func SameNegateA(c bool, x, y int) int {
	if c {
		return x * 7
	}
	return y * 13
}
func SameNegateB(c bool, x, y int) int {
	if !c {
		return y * 13
	}
	return x * 7
}

// Extra same: the loop above, variables declared and assigned in the other order.
func SameLoopA(n int) int {
	x, y := 1, 2
	for i := 0; i < n; i++ {
		x, y = x+y, x*3
	}
	return x - y
}
func SameLoopB(n int) int {
	q, p := 2, 1
	for k := 0; k < n; k++ {
		q, p = p*3, p+q
	}
	return p - q
}

// Extra same: closures over different outer names.
func SameClosureA(limit int) func(int) bool {
	return func(value int) bool { return value*2 < limit }
}
func SameClosureB(bound int) func(int) bool {
	return func(candidate int) bool { return candidate*2 < bound }
}

// Added in cohere (#jfvhbj7). Same: a local copied before it is used, which lifting removes.
func SameCopyA(a, b int) int { return (a - b) * 17 }
func SameCopyB(a, b int) int {
	difference := a - b
	scaled := difference * 17
	return scaled
}

// Different: a constant that differs.
func DiffConstantA(x int) int { return x*3 + 1 }
func DiffConstantB(x int) int { return x*3 + 2 }

// Different: integer division against float division, with no constant whose kind could tell them apart.
func DiffDivisionA(a, b int) int         { return a / b }
func DiffDivisionB(a, b float64) float64 { return a / b }
