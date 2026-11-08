// Package differences holds the loops where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: a labeled break or continue, a break that leaves only a nested loop, a panic in
// place of a return, and a defer of a closure whose own loop never ends.
package differences

func work() {}

func plausible(values []int) {
outer:
	for {
		defer work()
		for range values {
			break outer
		}
	}
	for {
		defer work()
		for range values {
			continue
		}
		for {
			break
		}
	}
again:
	for {
		defer work()
		continue again
	}
	for {
		defer work()
		panic("stop")
	}
	defer func() {
		for {
			defer work()
		}
	}()
}
