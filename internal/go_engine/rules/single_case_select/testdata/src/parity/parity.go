package parity

func produce() chan int { return nil }

func work() {}

func loopedReceives(channel chan int, results chan []int, flags chan bool, channels chan chan int) {
	var value int
	var ok bool

	// A for statement with no clauses around a sole select of one receiving case is reported at the for, and its
	// select is not reported again.
	for {
		select {
		case <-channel:
		}
	}
	for {
		select {
		case received := <-channel:
			_ = received
		}
	}
	for {
		select {
		case value = <-channel:
		}
	}
	for {
		select {
		case received, open := <-channel:
			_, _ = received, open
		}
	}
	for {
		select {
		case value, ok = <-channel:
		}
	}
	for {
		select {
		case <-produce():
			work()
			work()
		}
	}
	for {
		select {
		case result := <-results:
			_ = result
		}
	}

	// The pattern sees through parentheses around the receive, alone or on an assignment's right side.
	for {
		select {
		case (<-channel):
		}
	}
	for {
		select {
		case received := (<-channel):
			_ = received
		}
	}
	for {
		select {
		case <-(<-channels):
		}
	}

	// A label on the select, or on the for, is seen through.
labeledSelect:
	for {
	inner:
		select {
		case <-channel:
			break inner
		}
	}
	for {
	only:
		select {
		case <-flags:
			break only
		}
	}

	// A block of one statement around the select is seen through as its label is.
	for {
		{
			select {
			case <-channel:
			}
		}
	}
	for {
		{
		blocked:
			select {
			case <-channel:
				break blocked
			}
		}
	}

	// A for statement inside the case of another's select is a second finding.
	for {
		select {
		case <-channel:
			for {
				select {
				case <-flags:
				}
			}
		}
	}
	_, _ = value, ok
	goto labeledSelect
}

func singleCases(channel chan int, done chan struct{}, index int) {
	// A select of one case outside a loop is reported at its keyword, whatever the case communicates.
	select {
	case <-channel:
	}
	select {
	case channel <- 1:
	}
	select {
	default:
	}
	select {
	case received := <-channel:
		_ = received
	}

	// A send in the first query's place is no receive, so only its select is reported.
	for {
		select {
		case channel <- 0:
		}
	}

	// A default case communicates nothing, so only its select is reported.
	for {
		select {
		default:
		}
	}

	// A for statement with a condition, an init or a post statement is no `for {}`.
	for index < 10 {
		select {
		case <-channel:
		}
	}
	for index := 0; ; index++ {
		select {
		case <-channel:
		}
		_ = index
	}
	for ; ; index++ {
		select {
		case <-channel:
		}
	}
	for index := 0; index < 10; {
		select {
		case <-done:
		}
		_ = index
	}
	for true {
		select {
		case <-channel:
		}
	}

	// A body of two statements, at the top or in a block within, is no sole select.
	for {
		select {
		case <-channel:
		}
		work()
	}
	for {
		work()
		select {
		case <-channel:
		}
	}
	for {
		{
			select {
			case <-channel:
			}
			work()
		}
	}

	// The select nested in another statement of the body is not the body's.
	for {
		if index > 0 {
			select {
			case <-channel:
			}
		}
	}
	for {
		go func() {
			select {
			case <-channel:
			}
		}()
	}

	// A range is no for statement of the first query.
	for range channel {
		select {
		case <-channel:
		}
	}
}

func leftAlone(channel chan int, done chan struct{}) {
	// A select of no case or of two is no single case, in a for statement or not.
	select {}
	select {
	case <-channel:
	case <-done:
	}
	select {
	case <-channel:
	default:
	}
	for {
		select {
		case <-channel:
		case <-done:
		}
	}
	for {
		select {
		case <-channel:
		default:
		}
	}
	for {
	}
	for {
		{
		}
	}
}
