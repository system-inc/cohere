// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation and its dominator tree. The
// original stops judging a call at the first way out the call is not on, walking the ways out in block order, where a
// judgment that hung on that order, or on which call it met first, would show here as a finding one side makes and the
// other does not. These functions have many ways out, and many calls to themselves, only some on every way out.
package differences

// Found by both: the first call is on every way out, the later ones only on some.
func manyCalls(depth int) int {
	manyCalls(depth)
	switch {
	case depth > 3:
		return manyCalls(depth - 1)
	case depth > 1:
		return manyCalls(depth-2) + 1
	}
	return 0
}

// Found by neither: many ways out, returns and a panic, and the one call reaches only the last.
func manyExits(depth int) int {
	switch depth {
	case 0:
		return 0
	case 1:
		panic("one")
	case 2:
		return 2
	}
	return manyExits(depth - 1)
}

// Found by both: every way out, returns and a panic alike, comes after the call in a loop's header.
func loopHeader(rows int) int {
	for loopHeader(rows) > 0 {
		if rows == 7 {
			panic("seven")
		}
		rows--
	}
	return rows
}
