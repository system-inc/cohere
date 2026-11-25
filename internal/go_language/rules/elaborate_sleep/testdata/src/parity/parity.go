// Package parity holds every shape S1037's query and its judgment branch on, and the near miss of each.
package parity

import (
	"fmt"
	"time"
)

// emptyBody is the query's shape with an empty body, reported with the fix.
func emptyBody() {
	select { // reported, fixed
	case <-time.After(time.Second):
	}
}

// bodies are cases whose bodies are not empty, reported with no fix.
func bodies() {
	select { // reported, no fix
	case <-time.After(0):
		fmt.Println("waited")
	}
	select { // reported, no fix: a body of one empty block is no empty body
	case <-time.After(0):
		{
		}
	}
	select { // reported, no fix: a comment is no statement, and the body still holds one
	case <-time.After(0):
		// waiting
		fmt.Println()
	}
}

// comments are a case whose empty body holds only a comment, reported with the fix, which drops it, as the
// original's does.
func comments() {
	select { // reported, fixed
	case <-time.After(0):
		// nothing to do
	}
}

// parentheses are looked through around the receive, the call, the callee and the argument, and the argument is
// bound without its own.
func parentheses(duration time.Duration) {
	select { // reported, fixed
	case (<-time.After(duration)):
	}
	select { // reported, fixed
	case <-(time.After(duration)):
	}
	select { // reported, fixed
	case <-(time.After)(duration):
	}
	select { // reported, fixed, the argument's parentheses dropped
	case <-time.After((duration)):
	}
	select { // reported, fixed, the inner parentheses kept
	case <-time.After((duration + 1) * 2):
	}
}

// arguments are the argument's forms: a constant, a call, an expression across lines with a comment inside.
func arguments(durations []time.Duration, next func() time.Duration) {
	const pause = 5 * time.Millisecond
	select { // reported, fixed
	case <-time.After(pause):
	}
	select { // reported, fixed
	case <-time.After(next()):
	}
	select { // reported, fixed
	case <-time.After(durations[0] +
		// the second
		durations[1]):
	}
}

// places are selects that stand in a loop, a function literal, a labeled statement and another select's case.
func places() {
	for index := 0; index < 3; index++ {
		select { // reported, fixed
		case <-time.After(0):
		}
	}
	wait := func() {
		select { // reported, fixed
		case <-time.After(0):
		}
	}
	wait()
	goto pause
pause:
	select { // reported, fixed
	case <-time.After(0):
	}
	select { // reported, no fix
	case <-time.After(0):
		select { // reported, fixed
		case <-time.After(0):
		}
	}
}

// nearMisses are selects the query leaves alone.
func nearMisses(channel chan int, after func(time.Duration) <-chan time.Time) {
	var received time.Time
	select {}
	select {
	case <-time.After(0):
	case <-channel:
	}
	select {
	case <-time.After(0):
	default:
	}
	select {
	default:
	}
	select {
	case value := <-time.After(0):
		fmt.Println(value)
	}
	select {
	case received = <-time.After(0):
	}
	select {
	case received, _ = <-time.After(0):
	}
	select {
	case <-channel:
	}
	select {
	case channel <- 1:
	}
	select {
	case <-time.Tick(0):
	}
	select {
	case <-time.NewTimer(0).C:
	}
	select {
	case <-after(0):
	}
	afterValue := time.After
	select {
	case <-afterValue(0):
	}
	select {
	case <-After(0):
	}
	fmt.Println(received)
}

// After is the package's own function spelled as time's, which the query's Symbol does not name.
func After(time.Duration) <-chan time.Time { return nil }

// localTime shadows the package name time after the select, which still names the import where it stands, so the
// fix writes time.Sleep, which the original writes too.
func localTime() {
	select { // reported, fixed
	case <-time.After(0):
	}
	time := 1
	fmt.Println(time)
}
