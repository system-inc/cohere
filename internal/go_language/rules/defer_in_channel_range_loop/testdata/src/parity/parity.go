// Package parity holds the range loops SA9001 judges, flagged and left alone, for the port and the original to judge
// side by side: loops over channels of every shape the core type reads, over things that are not channels, defers at
// every depth, and the returns and breaks that spare a loop.
package parity

type stream chan int

type receiver <-chan string

func work() {}

func flagged(channel chan int, named stream, receiveOnly <-chan bool, alias receiver, values []int) {
	for range channel {
		defer work()
	}
	for value := range named {
		defer work()
		_ = value
	}
	for range receiveOnly {
		if len(values) > 0 {
			defer work()
			defer work()
		}
	}
	for range alias {
		for range values {
			defer work()
		}
	}
	for range channel {
		go func() {
			defer work()
			return
		}()
		defer work()
	}
	for range channel {
		for range channel {
			defer work()
		}
	}
	for range (<-chan int)(channel) {
		defer work()
	}
	for range channel {
		defer work()
		continue
	}
	for range channel {
		defer work()
		if len(values) > 0 {
			return
		}
		continue
	}
}

func generic[Channel chan int](channel Channel) {
	for range channel {
		defer work()
	}
}

func spared(channel chan int, values []int, table map[string]int) {
	for range values {
		defer work()
	}
	for range table {
		defer work()
	}
	for range channel {
		work()
	}
	for range channel {
		defer work()
		if len(values) > 0 {
			return
		}
	}
	for range channel {
		defer work()
		if len(values) > 0 {
			break
		}
	}
	for range channel {
		defer work()
		switch {
		case len(values) > 0:
			break
		}
	}
	for range 3 {
		defer work()
	}
}
