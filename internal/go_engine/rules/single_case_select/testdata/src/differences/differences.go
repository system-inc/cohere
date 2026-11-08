// Package differences holds the shapes where the port and the original could most plausibly part, and records no
// difference: the two agree on every one.
package differences

type pipe chan int

func generic[Channel ~chan int, Receiver ~<-chan int](channel Channel, receiver Receiver) {
	// A receive from a type parameter's channel is a receive, by syntax alone.
	for {
		select {
		case <-channel:
		}
	}
	for {
		select {
		case received := <-receiver:
			_ = received
		}
	}
	var named pipe
	for {
		select {
		case <-named:
		}
	}
}

func labelsAndBlocks(channel chan int) {
	// Two labels on the select are both seen through, and the select bound is the select, never a label. A label on a
	// label or on a block is no target of break, so goto names them.
	for {
	first:
	second:
		select {
		case <-channel:
			if channel == nil {
				goto first
			}
			break second
		}
	}

	// A labeled block of one statement, and blocks of one statement within blocks of one, are seen through.
	for {
	block:
		{
			select {
			case <-channel:
				goto block
			}
		}
	}
	for {
		{
			{
				select {
				case <-channel:
				}
			}
		}
	}

	// A labeled for statement is the body's sole statement, and it is no select, so only the inner for is reported.
	for {
	loop:
		for {
			select {
			case <-channel:
				break loop
			}
		}
	}
}

func assignments(channel chan int, values []int, record map[string]int) {
	// Any left side of an assignment is the same to the query, a blank, an index or a map entry.
	for {
		select {
		case _ = <-channel:
		}
	}
	for {
		select {
		case values[0] = <-channel:
		}
	}
	for {
		select {
		case record["key"], _ = <-channel:
		}
	}
}

func nested(channel chan int, done chan struct{}) {
	// A select of one case inside the case of a for statement's select is reported for itself.
	for {
		select {
		case <-channel:
			select {
			case <-done:
			}
		}
	}

	// A receive standing alone in the body is no select.
	for {
		<-channel
	}
}

func closures(channel chan int) {
	// A select in a function literal the body calls is not the body's.
	for {
		func() {
			select {
			case <-channel:
			}
		}()
	}
}

var literal = func(channel chan int) {
	for {
		select {
		case <-channel:
		}
	}
}
