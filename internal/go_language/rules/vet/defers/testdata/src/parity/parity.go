// Package parity holds every shape of a defer that the defers pass judges, those it reports and those it leaves
// alone beside them, so the original and the port can be held to the same findings.
package parity

import (
	"fmt"
	stdtime "time"

	faketime "faketime"
)

func record(duration stdtime.Duration) {}

func recordSeconds(seconds float64) {}

func recordAll(durations ...stdtime.Duration) {}

func apply(measure func(stdtime.Time) stdtime.Duration, start stdtime.Time) {}

func identity[Value any](value Value) Value {
	return value
}

type recorder struct {
	since func(stdtime.Time) stdtime.Duration
}

func (recorder) record(duration stdtime.Duration) {}

func (recorder) chained(duration stdtime.Duration) recorder {
	return recorder{}
}

type measurer interface {
	Measure(stdtime.Duration)
}

// since holds time.Since as a value, so a call through it resolves to the variable, not to time.Since.
var since = stdtime.Since

// The deferred call itself, under an import alias, with parentheses around the function.
func deferredItself(start stdtime.Time) {
	defer stdtime.Since(start)   // want "call to time.Since is not deferred"
	defer (stdtime.Since)(start) // want "call to time.Since is not deferred"
	defer stdtime.Until(start)
	defer stdtime.Now()
}

// time.Since among the arguments, at every depth.
func inArguments(start stdtime.Time) {
	defer record(stdtime.Since(start))                                          // want "call to time.Since is not deferred"
	defer recordSeconds(stdtime.Since(start).Seconds())                         // want "call to time.Since is not deferred"
	defer recordSeconds(float64(stdtime.Since(start)))                          // want "call to time.Since is not deferred"
	defer fmt.Println(stdtime.Since(start) > stdtime.Second)                    // want "call to time.Since is not deferred"
	defer fmt.Println(-stdtime.Since(start))                                    // want "call to time.Since is not deferred"
	defer fmt.Println([]stdtime.Duration{stdtime.Since(start)})                 // want "call to time.Since is not deferred"
	defer fmt.Println(map[string]stdtime.Duration{"a": stdtime.Since(start)})   // want "call to time.Since is not deferred"
	defer fmt.Println([]int{1, 2}[int(stdtime.Since(start))%2])                 // want "call to time.Since is not deferred"
	defer fmt.Println(struct{ elapsed stdtime.Duration }{stdtime.Since(start)}) // want "call to time.Since is not deferred"
	defer recordAll(stdtime.Since(start), stdtime.Since(start))                 // want "call to time.Since is not deferred" "call to time.Since is not deferred"
	defer recordAll([]stdtime.Duration{stdtime.Since(start)}...)                // want "call to time.Since is not deferred"
	defer record(identity(stdtime.Since(start)))                                // want "call to time.Since is not deferred"
	defer record(identity[stdtime.Duration](stdtime.Since(start)))              // want "call to time.Since is not deferred"
	defer record(stdtime.Since(stdtime.Now().Add(-stdtime.Since(start))))       // want "call to time.Since is not deferred" "call to time.Since is not deferred"
	defer record(stdtime.Until(start))
	defer record(stdtime.Now().Sub(start))
	defer record(faketime.Since(start))
}

// time.Since in the function value of the deferred call, as its receiver or inside it.
func inFunctionValue(start stdtime.Time, values []recorder, measured measurer) {
	defer fmt.Println(stdtime.Since(start).String())              // want "call to time.Since is not deferred"
	defer stdtime.Since(start).Round(stdtime.Second)              // want "call to time.Since is not deferred"
	defer recorder{}.chained(stdtime.Since(start)).record(0)      // want "call to time.Since is not deferred"
	defer values[int(stdtime.Since(start))%len(values)].record(0) // want "call to time.Since is not deferred"
	defer measured.Measure(stdtime.Since(start))                  // want "call to time.Since is not deferred"
	defer recorder{}.record(stdtime.Since(start))                 // want "call to time.Since is not deferred"
	defer (*recorder).record(nil, stdtime.Since(start))           // want "call to time.Since is not deferred"
	method := recorder{}.record
	defer method(stdtime.Since(start)) // want "call to time.Since is not deferred"
}

// time.Since as a value, through a variable or a field, is not a call to time.Since.
func asValue(start stdtime.Time, holder recorder) {
	defer apply(stdtime.Since, start)
	defer record(since(start))
	defer record(holder.since(start))
	measure := stdtime.Since
	defer record(measure(start))
}

// Function literals: what they hold is evaluated when they run, but what is passed to them is not.
func inFunctionLiterals(start stdtime.Time) {
	defer func() { record(stdtime.Since(start)) }()
	defer func(elapsed stdtime.Duration) { record(elapsed) }(stdtime.Since(start)) // want "call to time.Since is not deferred"
	defer apply(func(stdtime.Time) stdtime.Duration { return stdtime.Since(start) }, start)
	defer record(func() stdtime.Duration { return stdtime.Since(start) }())
	defer func() {
		defer record(stdtime.Since(start)) // want "call to time.Since is not deferred"
	}()
	measure := func() {
		defer record(stdtime.Since(start)) // want "call to time.Since is not deferred"
		go record(stdtime.Since(start))
	}
	measure()
}

// A go statement and a plain call are not defer statements.
func notDeferred(start stdtime.Time) {
	go record(stdtime.Since(start))
	record(stdtime.Since(start))
	elapsed := stdtime.Since(start)
	defer record(elapsed)
}

// Defer statements in every block a function can hold, and in a generic function.
func inBlocks[Value any](start stdtime.Time, value Value, channel chan int) {
	if value := any(value); value != nil {
		defer record(stdtime.Since(start)) // want "call to time.Since is not deferred"
	}
	for range 2 {
		defer record(stdtime.Since(start)) // want "call to time.Since is not deferred"
	}
	switch any(value).(type) {
	case int:
		defer record(stdtime.Since(start)) // want "call to time.Since is not deferred"
	}
	select {
	case <-channel:
		defer record(stdtime.Since(start)) // want "call to time.Since is not deferred"
	default:
	}
	{
		defer fmt.Println(identity(value), stdtime.Since(start)) // want "call to time.Since is not deferred"
	}
}

// A generic type's method.
type box[Value any] struct{ value Value }

func (container box[Value]) measure(start stdtime.Time) {
	defer fmt.Println(container.value, stdtime.Since(start)) // want "call to time.Since is not deferred"
}

// Builtins are callees too, and none is time.Since.
func builtins(start stdtime.Time) {
	durations := []stdtime.Duration{}
	defer fmt.Println(len(durations))
	defer copy(durations, []stdtime.Duration{stdtime.Since(start)}) // want "call to time.Since is not deferred"
	defer panic(stdtime.Since(start))                               // want "call to time.Since is not deferred"
	defer recover()
}

// A local function named Since is not time.Since, and neither is one shadowing the import's name.
func shadowed(start stdtime.Time) {
	Since := func(stdtime.Time) stdtime.Duration { return 0 }
	defer record(Since(start))
	stdtime := recorder{since: Since}
	defer record(stdtime.since(start))
}
