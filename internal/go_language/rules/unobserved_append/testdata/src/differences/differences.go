// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation. The original judges the slice an
// append extends by ranging over a map of the values it was built from, in an order Go leaves to chance, where its
// translation ranges over them in the order reached. These appends are built from many values, through loops and
// phis, some with a reader that refuses the append reachable from only some of them, so an outcome that hung on the
// order would show here as a finding one side makes and the other does not.
package differences

func keep(values []int) {}

// Found by both: one append built from many origins, constant, makes, slicings of a local array and of a make,
// merged through a switch and a loop.
func manyOrigins(rows int, condition bool) {
	var values []int
	switch {
	case condition:
		values = make([]int, 0, rows)
	case rows > 4:
		var array [8]int
		values = array[:0]
	case rows > 2:
		values = make([]int, rows)[:1]
	}
	for row := 0; row < rows; row++ {
		values = append(values, row)
	}
}

// Found by both: two appends in a loop, each among the other's origins through two phis.
func chainInLoop(rows int) {
	values := make([]int, 0, rows)
	for row := 0; row < rows; row++ {
		if row%2 == 0 {
			values = append(values, row)
		}
		values = append(values, -row)
	}
}

// Found by neither: the inner loop's phi of both appends is sliced, a read of their results.
func twoAppendsOfOnePhi(rows int, columns int) {
	var values []int
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			if column%2 == 0 {
				values = append(values[:0], column)
			} else {
				values = append(values, row)
			}
		}
	}
}

// Found by neither: one origin, deep in the branches, is passed to a call.
func oneOriginShared(rows int, condition bool) {
	var values []int
	if condition {
		values = make([]int, 0, rows)
		keep(values)
	} else if rows > 2 {
		values = make([]int, rows)
	}
	for row := 0; row < rows; row++ {
		values = append(values, row)
	}
}

// Found by neither: an origin's slicing is read, reached only through the slicing of another origin.
func slicingRead(rows int) int {
	var array [8]int
	values := array[:0]
	head := values[:1]
	for row := 0; row < rows; row++ {
		values = append(values, row)
	}
	return head[0]
}

// Found by neither: two appends extend one make, each a reader of the other's origin.
func sharedOrigins() {
	base := make([]int, 0, 4)
	first := append(base, 1)
	second := append(base, 2)
	first = append(first, 3)
	second = append(second, 4)
}
