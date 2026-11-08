// Package parity holds every shape the pattern accepts or refuses: for loops with each of their three clauses, a
// range loop, a body of one select and of more, the select under a label and in a block, and default cases empty,
// holding a statement, holding only a comment, and missing.
package parity

func loops(channel chan int, done chan struct{}, condition bool) {
	for {
		select {
		case <-channel:
		default:
		}
	}
	for {
		select {
		default:
		case <-channel:
		}
	}
	for {
		select {
		default:
		}
	}
	for condition {
		select {
		case <-channel:
		default:
		}
	}
	for index := 0; ; {
		_ = index
		select {
		default:
		}
	}
	for ; ; condition = !condition {
		select {
		default:
		}
	}
	for range channel {
		select {
		default:
		}
	}
	for {
		select {
		case <-channel:
		default:
		}
		_ = condition
	}
	for {
	label:
		select {
		case <-done:
			break label
		default:
		}
	}
	for {
		{
			select {
			default:
			}
		}
	}
	for {
		{
			{
				select {
				case value := <-channel:
					_ = value
				default:
				}
			}
		}
	}
	for {
		select {
		case <-channel:
		default:
			condition = true
		}
	}
	for {
		select {
		case <-channel:
		default:
			// Waiting.
		}
	}
	for {
		select {
		case <-channel:
		}
	}
	for {
		select {}
	}
	select {
	case <-channel:
	default:
	}
}

func nested(channel chan int) {
	for {
		func() {
			for {
				select {
				default:
				}
			}
		}()
	}
}

func generic[T any](channel chan T) {
	for {
		select {
		case <-channel:
		default:
		}
	}
}
