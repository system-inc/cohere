// Package parity holds the if/else if chains SA4014 judges, flagged and left alone, for the port and the original to
// judge side by side: repeats of every kind of condition, a third and fourth repeat, chains a side effect or an
// initializer stops part of the way, nested chains, and conditions written differently that render the same.
package parity

type point struct {
	x int
	y int
}

func chains(first bool, second bool, count int, values []int, location point, channel chan bool, boxed interface{}) {
	if first {
	} else if first {
	}
	if first {
	} else if second {
	} else if first {
	} else if first {
	} else if first {
	}
	if count > 1 {
	} else if count < 0 {
	} else if count > 1 {
	}
	if values[0] == 1 {
	} else if location.x == 2 {
	} else if values[0] == 1 {
	} else if location.x == 2 {
	}
	if boxed == nil {
	} else if boxed == nil {
	}
	if count == 1 {
	} else if count == 1 {
	}
	if first && second {
	} else if first &&
		second {
	}
	if first {
	} else if first {
	} else {
		if second {
		} else if second {
		}
	}
	if first {
		if count > 0 {
		} else if count > 0 {
		}
	} else if second {
	}
	if <-channel {
	} else if first {
	} else if first {
	}
	if first {
	} else if len(values) > 0 {
	} else if first {
	}
	if first {
	} else if !first {
	} else if second {
	} else if !first {
	}
}

func stopped(first bool, second bool, count int) {
	if first {
	} else if size := count; size > 0 {
	} else if second {
	} else if second {
	}
	if first {
	} else if gate() {
	} else if second {
	} else if second {
	}
	if first {
	} else if second {
	}
	if first {
	}
	if first {
	} else {
	}
}

func gate() bool { return true }
