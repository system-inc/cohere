// Package differences holds the go statements where the port's test of syntax could most plausibly part from the
// pattern: blocks nested around the call, a label over a block, parentheses around the function literal and around
// the call, and Add reached through a method expression and an embedding. The port and the original find the same
// here, so the fixture records no difference.
package differences

import "sync"

type holder struct {
	*sync.WaitGroup
}

func shapes(wg *sync.WaitGroup, held holder) {
	go func() {
	label:
		{
			wg.Add(1)
		}
		goto label
	}()
	go (func() {
		(wg.Add(1))
	})()
	go func() {
		(*sync.WaitGroup).Add(wg, 1)
	}()
	go func() {
		held.Add(1)
	}()
}
