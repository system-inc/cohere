// Package differences holds the loops where the port and the original could most plausibly part, and records no
// difference, since they part nowhere. A continue after the loop's return or break leaves the original judging that
// the loop never ends, so both report defers in loops that can end: a finding the port keeps, since a finding the
// original makes is never one the port may drop. A labeled break, a goto and a type parameter constrained to every
// type whose underlying type is a channel are here too.
package differences

func work() {}

// continueAfterReturn is the false positive the original makes, and the port with it: the loop returns when values
// is not empty, so its defer can run, yet the continue after the return is the walk's last branch statement, and it
// judges the loop as never ending.
func continueAfterReturn(channel chan int, values []int) {
	for range channel {
		defer work()
		if len(values) > 0 {
			return
		}
		continue
	}
}

func plausible(channel chan int, values []int) {
	for range channel {
		defer work()
		if len(values) > 0 {
			break
		}
		if len(values) > 1 {
			continue
		}
	}
outer:
	for range channel {
		defer work()
		for range values {
			break outer
		}
	}
	for range channel {
		defer work()
		goto done
	}
done:
}

func approximate[Channel ~chan int](channel Channel) {
	for range channel {
		defer work()
	}
}
