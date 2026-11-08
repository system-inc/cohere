// Package parity holds the shapes SA1015 depends on, each with a comment saying whether the two are expected to find
// it: functions that return and that never do, the call made directly, through a function value and in literals,
// goroutines and methods, beside a test file whose functions both leave alone.
package parity

import "time"

// Found by both: a function that returns after ranging over the ticker.
func returns() {
	for range time.Tick(time.Second) {
		break
	}
}

// Found by neither: an endless function.
func endless() {
	for range time.Tick(time.Second) {
		println("")
	}
}

// Found by both: the ticker's channel kept, and the function returns.
func kept() <-chan time.Time {
	return time.Tick(time.Second)
}

// Found by both, twice: two calls in a function that returns.
func twice() {
	first := time.Tick(time.Second)
	second := time.Tick(time.Minute)
	<-first
	<-second
}

// Found by neither: a function that panics on one path and loops forever on the other does not return.
func panicsOrLoops(condition bool) {
	if condition {
		panic("stop")
	}
	for range time.Tick(time.Second) {
		println("")
	}
}

// Found by both: time.Tick held in a variable is the function itself.
func functionValue() {
	tick := time.Tick
	<-tick(time.Second)
}

// Found by neither: time.NewTicker can be stopped.
func newTicker() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	<-ticker.C
}

// Found by both, in the literal: a goroutine that returns.
func goroutineReturns() {
	go func() {
		<-time.Tick(time.Second)
	}()
}

// Found by neither: an endless goroutine, in a function that returns.
func goroutineEndless() {
	go func() {
		for range time.Tick(time.Second) {
			println("")
		}
	}()
}

// Found by both: a literal called in place that returns.
func literal() {
	func() {
		<-time.Tick(time.Second)
	}()
}

type poller struct{ interval time.Duration }

// Found by both: a method that returns.
func (value *poller) once() {
	<-time.Tick(value.interval)
}

// Found by neither: an endless method.
func (value poller) forever() {
	for range time.Tick(value.interval) {
		println("")
	}
}

// Found by both: a loop over the ticker left by a return.
func returnsFromLoop(limit int) int {
	count := 0
	for range time.Tick(time.Second) {
		count++
		if count > limit {
			return count
		}
	}
	return count
}

// Found by both: a select over the ticker and a stop channel.
func selects(stop chan struct{}) {
	ticker := time.Tick(time.Second)
	for {
		select {
		case <-ticker:
			println("")
		case <-stop:
			return
		}
	}
}
