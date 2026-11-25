// Package differences holds the breaks where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: a switch with an initializer, a select with an empty case before the break's,
// a break inside a labeled switch, and an if ending a case whose else is an else if holding a break.
package differences

func plausible(values []int, channel chan int) {
	for {
		switch size := len(values); size {
		case 0:
			break
		}
	}
	for {
		select {
		case <-channel:
		case channel <- 1:
			break
		}
	}
	for {
	inner:
		switch {
		case true:
			break inner
		}
	}
	for {
		switch {
		case true:
			if true {
				println()
			} else if false {
				break
			}
		}
	}
}
