// Package parity holds the breaks SA4011 judges, flagged and left alone, for the port and the original to judge side
// by side: breaks ending switch and select cases in for and range loops, inside an if and its else, and the breaks
// it leaves alone, labeled, not last, nested deeper, in a type switch, or in a switch outside any loop.
package parity

func flagged(values []int, channel chan int, boxed interface{}) {
	for index := 0; index < 3; index++ {
		switch index {
		case 0:
			println(index)
			break
		}
	}
	for range values {
		select {
		case <-channel:
			break
		default:
			println()
			break
		}
	}
	for _, value := range values {
		switch {
		case value > 0:
			if value > 1 {
				println(value)
				break
			} else {
				println(value)
				break
			}
		}
	}
	for {
		switch {
		case len(values) > 0:
			if len(values) > 1 {
				break
			}
		}
	}
	for len(values) > 0 {
		switch {
		case true:
			if true {
				break
			} else if false {
				break
			}
		}
	}
	for {
		switch {
		case true:
			if true {
				break
			} else {
			}
		}
	}
	for {
		go func() {
			for {
				switch {
				default:
					break
				}
			}
		}()
	}
}

func spared(values []int, channel chan int, boxed interface{}) {
	for {
		switch {
		case true:
			break
			println()
		}
	}
	for {
		switch boxed.(type) {
		case int:
			break
		}
	}
	for {
		if len(values) > 0 {
			switch {
			case true:
				break
			}
		}
	}
	for {
		switch {
		case true:
			{
				break
			}
		}
	}
	for {
		switch {
		case true:
			if true {
			} else {
				break
			}
		}
	}
	for {
		switch {
		case true:
			continue
		}
	}
	switch {
	case true:
		break
	}
	select {
	case <-channel:
		break
	}
}
