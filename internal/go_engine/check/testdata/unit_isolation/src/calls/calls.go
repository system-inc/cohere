// Package calls is the unit-isolation test's own fixture (#h350vqr): units that call one another, so a variant that
// stubs a callee and keeps its caller shows whether the caller's findings rest on the callee's body or only on its
// answers. The pin stubs halt, proceed and double and keeps their callers, whose findings each rest on one fact or
// answer about the callee: halt never returns, proceed does, and double is pure.
package calls

import (
	"context"
	"os"
	"strings"
)

// halt never returns, which ctrlflow reads from its body.
func halt() {
	os.Exit(1)
}

// proceed returns, though its stub, a panic, would not.
func proceed() {
	println("proceeding")
}

// double is pure, which the resolver judges from its body.
func double(value int) int {
	return value * 2
}

// compute is a value nothing below reads past halt.
func compute() int {
	return len(os.Args)
}

// Unreached assigns a value that only code after halt reads, so the value is never used because halt never returns.
func Unreached() int {
	value := compute()
	halt()
	return value
}

// Leaks drops its cancel function on the path that returns early, a path that exists because proceed returns.
func Leaks(parent context.Context) context.Context {
	child, cancel := context.WithCancel(parent)
	proceed()
	if child.Err() != nil {
		return nil
	}
	cancel()
	return child
}

// Discards calls double, which is pure, and drops its result.
func Discards() {
	double(2)
}

// Joins calls a callee in another file of its package and one of the standard library's.
func Joins(parts []string) string {
	return strings.Join(parts, separator())
}

// Counter counts, through methods that call one another.
type Counter struct {
	count int
}

// Increment adds one through Add.
func (counter *Counter) Increment() {
	counter.Add(1)
}

// Add adds amount.
func (counter *Counter) Add(amount int) {
	counter.count += amount
}

// Mapped is each of values transformed, a generic function its callers instantiate.
func Mapped[Value any, Result any](values []Value, transform func(Value) Result) []Result {
	results := make([]Result, 0, len(values))
	for _, value := range values {
		results = append(results, transform(value))
	}
	return results
}

// Doubled instantiates Mapped with double.
func Doubled(values []int) []int {
	return Mapped(values, double)
}

// first is the first of values, or zero when there is none, which a caller compares with nil.
func first(values []*int) *int {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

// Firsts reads through what first returned, after asking whether it was nil.
func Firsts(values []*int) int {
	pointer := first(values)
	if pointer == nil {
		println("none")
	}
	return *pointer
}
