// Package main is a command, where returning from main.main ends the process, so main alone goes unjudged.
package main

import "context"

var maybe bool

// main is not judged: returning from it ends the process.
func main() {
	_, cancel := context.WithCancel(context.Background())
	if maybe {
		cancel()
	}
	// A function literal inside main is judged, since it returns to main, not out of the process.
	func() {
		_, inner := context.WithCancel(context.Background()) // finds: not used on all paths
		if maybe {
			inner()
		}
	}() // finds: this return
}

// command is a type with a method named main, which is judged: only the function main.main ends the process.
type command struct{}

func (command) main() {
	_, cancel := context.WithCancel(context.Background()) // finds: not used on all paths
	if maybe {
		cancel()
	}
} // finds: this return

// discarded is judged in package main as anywhere else.
func discarded() {
	ctx, _ := context.WithCancel(context.Background()) // finds: discarded
	_ = ctx
}

func init() {
	_, cancel := context.WithTimeout(context.Background(), 0) // finds: not used on all paths
	if maybe {
		cancel()
	}
} // finds: this return
