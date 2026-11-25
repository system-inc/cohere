package parity

import (
	"bytes"
	"fmt"
	"testing"
)

// embeddedBenchmark promotes testing.B's Loop.
type embeddedBenchmark struct{ *testing.B }

// stopwatch has a Loop of its own, which keeps nothing alive.
type stopwatch struct{}

func (stopwatch) Loop() bool { return false }

// benchmark is another name for testing.B.
type benchmark = testing.B

// Only a statement directly in the body of a loop whose condition calls testing.B's Loop is exempt.
func benchmarkLoops(b *testing.B, aliased *benchmark, written *bytes.Buffer) {
	for b.Loop() {
		written.String() // clean
		fmt.Sprint(1)    // clean
		(fmt.Sprint(1))  // clean
		{
			fmt.Sprint(1) // reported: its parent is a block inside the body
		}
		if b.N > 0 {
			fmt.Sprint(1) // reported
		}
		switch {
		default:
			fmt.Sprint(1) // reported: its parent is a case clause
		}
		func() {
			fmt.Sprint(1) // reported: its grandparent is a literal
		}()
		goto exempt
	exempt:
		fmt.Sprint(1) // reported: its parent is a labeled statement
	}
	for b.Loop() {
		for index := 0; index < 1; index++ {
			fmt.Sprint(1) // reported: an inner loop's body
		}
	}
	for b.Loop() {
		fmt.Sprint(1) // clean: the condition in parentheses
	}
	for index := 0; b.Loop(); index++ {
		fmt.Sprint(index) // clean
	}
	for (*testing.B).Loop(b) {
		fmt.Sprint(1) // clean: a method expression of Loop
	}
	for (b.Loop)() {
		fmt.Sprint(1) // clean
	}
	for aliased.Loop() {
		fmt.Sprint(1) // clean: through an alias
	}
	embedded := embeddedBenchmark{b}
	for embedded.Loop() {
		fmt.Sprint(1) // clean: a promoted Loop
	}
	loop := b.Loop
	for loop() {
		fmt.Sprint(1) // reported: a function value has no declared callee
	}
	for b.Loop() && b.N > 0 {
		fmt.Sprint(1) // reported: the condition is no call
	}
	for !b.Loop() {
		fmt.Sprint(1) // reported
	}
	for range b.N {
		fmt.Sprint(1) // reported: a range loop
	}
	for {
		fmt.Sprint(1) // reported: no condition
		break
	}
	var watch stopwatch
	for watch.Loop() {
		fmt.Sprint(1) // reported: another type's Loop
	}
	fmt.Sprint(1) // reported
}
