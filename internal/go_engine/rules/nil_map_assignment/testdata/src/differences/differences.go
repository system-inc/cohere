// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation through the same irutil.Flatten.
// The one judgment the original makes is what a map flattens to through its phis, so these maps pass through many
// phis, in loops and in branches, where a port that walked them any other way would show a finding one side makes
// and the other does not.
package differences

// Found by both: nil through a switch, a loop and a nested loop, each a phi of only that nil.
func throughPhis(rows int, choice int) {
	var entries map[int]int
	switch choice {
	case 1:
		entries = nil
	case 2:
	}
	for row := 0; row < rows; row++ {
		for column := 0; column < row; column++ {
			entries[row] = column
		}
	}
}

// Found by neither: made only deep in a loop, so the loop's phi brings the make as well as the nil.
func madeDeepInLoop(rows int) {
	var entries map[int]int
	for row := 0; row < rows; row++ {
		entries[row] = row
		if row > 4 {
			entries = make(map[int]int)
		}
	}
}

// Found by neither: two phis that reach each other in a loop, one made on its way round.
func phisReachingEachOther(rows int) {
	var first, second map[int]int
	for row := 0; row < rows; row++ {
		first, second = second, first
		if row == 3 {
			second = map[int]int{}
		}
		first[row] = row
	}
}

// Found by neither, for all the map is only ever nil: the two phis that reach each other in a loop do not flatten
// to one constant.
func nilPhisReachingEachOther(rows int) {
	var first, second map[int]int
	for row := 0; row < rows; row++ {
		first, second = second, first
		first[row] = row
	}
}

// Found by both: the same two phis, with the second map the first's own nil, so both flatten to that one constant.
// The builder leaves these phis in place, since each brings the other, so only a walk through them finds the nil.
func onePhiNilReachingEachOther(rows int) {
	var first map[int]int
	second := first
	for row := 0; row < rows; row++ {
		first, second = second, first
		first[row] = row
	}
}
