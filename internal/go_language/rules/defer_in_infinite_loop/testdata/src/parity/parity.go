// Package parity holds the loops SA5003 judges, flagged and left alone, for the port and the original to judge side
// by side: defers at every depth of a loop with no condition, in nested loops and closures, and the returns and
// breaks that spare a loop wherever they sit.
package parity

func work() {}

func flagged(values []int, channel chan int) {
	for {
		defer work()
	}
	for {
		defer work()
		defer work()
	}
	for {
		if len(values) > 0 {
			defer work()
		}
	}
	for {
		switch len(values) {
		case 0:
			defer work()
		default:
			continue
		}
	}
	for {
		select {
		case <-channel:
			defer work()
		}
	}
	for {
		for {
			defer work()
		}
	}
	for {
		for index := 0; index < 3; index++ {
			defer work()
		}
	}
	for {
		for range values {
			defer work()
		}
	}
	for {
		defer func() {
			return
		}()
	}
	for {
		go func() {
			defer work()
		}()
		defer work()
	}
	for index := 0; ; index++ {
		defer work()
	}
	for {
		defer work()
		goto done
	}
done:
	for {
		defer work()
		continue
	}
}

func spared(values []int, channel chan int) {
	for len(values) > 0 {
		defer work()
	}
	for {
		defer work()
		if len(values) > 0 {
			return
		}
	}
	for {
		defer work()
		switch {
		case len(values) > 0:
			break
		}
	}
	for {
		select {
		case <-channel:
			defer work()
		default:
			break
		}
	}
	for {
		for range values {
			break
		}
		defer work()
	}
	for {
		work()
	}
}
