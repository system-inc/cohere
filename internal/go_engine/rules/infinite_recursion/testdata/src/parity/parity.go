// Package parity holds the shapes SA5007 depends on: which calls have the function itself as their static callee, and
// where in its control flow each one is. Each function's comment says what the original makes of it; the parity test
// holds only that the port makes the same.
package parity

import "os"

type counter struct {
	count int
}

type caller interface {
	Call()
}

func use(values ...any) {}

// Found: the call is the whole body.
func whole() {
	whole()
}

// Found: a pointer receiver calling itself.
func (target *counter) Increment() {
	target.count++
	target.Increment()
}

// Not found: a function with a deferred call has a recover block, a second entry and a way out that no call
// dominates, so the deferred call is not on every way out.
func deferred() {
	defer deferred()
}

// Not found: a goroutine started on itself.
func spawned() {
	go spawned()
}

// Found: after a loop that may not end, the call is still on every way out.
func afterLoop(rows int) {
	for rows > 0 {
		rows--
	}
	afterLoop(rows)
}

// Found: a function that only loops has no way out, so every call in it is on all of none.
func loopsForever() {
	for {
		loopsForever()
	}
}

// Not found: the loop can break, and the call is inside it.
func loopThatBreaks(rows int) {
	for row := 0; row < rows; row++ {
		if row == 3 {
			break
		}
		loopThatBreaks(row)
	}
}

// Not found: the call is on one branch.
func guarded(depth int) int {
	if depth == 0 {
		return 0
	}
	return guarded(depth-1) + 1
}

// Not found: both branches call, but neither call alone is on every way out.
func bothBranchesThenReturn(condition bool) {
	if condition {
		bothBranchesThenReturn(condition)
	} else {
		bothBranchesThenReturn(!condition)
	}
}

// Found: the call comes before the only check that could end it.
func checkedAfter(depth int) int {
	result := checkedAfter(depth - 1)
	if depth == 0 {
		return 0
	}
	return result
}

// Not found: a panic on the other branch is a way out the call is not on.
func panicsOtherwise(depth int) {
	if depth > 0 {
		panicsOtherwise(depth - 1)
	} else {
		panic("bottom")
	}
}

// Not found: os.Exit on the other branch ends the function too.
func exitsOtherwise(depth int) {
	if depth > 0 {
		exitsOtherwise(depth - 1)
		return
	}
	os.Exit(0)
}

// Not found: a call through an interface has no static callee.
func (target *counter) Call() {
	var through caller = target
	through.Call()
}

// Not found: a method value is a bound closure, not the method.
func (target *counter) Bound() {
	bound := target.Bound
	bound()
}

// Found: a method expression's call is the method itself.
func (target *counter) Expression() {
	(*counter).Expression(target)
}

// Not found: a function value has no static callee.
func throughValue() {
	value := throughValue
	value()
}

// Not found: one calls the other, and neither calls itself.
func mutualFirst() {
	mutualSecond()
}

func mutualSecond() {
	mutualFirst()
}

// Not found: the closure calls its outer function, not itself.
func outer() {
	func() {
		outer()
	}()
}

// Found once: the closure calls itself through a variable, no static call, and the outer function calls itself.
func closureThenSelf() {
	var recurse func()
	recurse = func() {
		recurse()
	}
	use(recurse)
	closureThenSelf()
}

// Not found: a generic function calling itself with its own type parameter, a callee the IR does not make the function.
func generic[Value any](value Value) {
	generic(value)
}

// Not found: the same with another type argument.
func genericOther[Value any](value Value) {
	genericOther(0)
}

// Found: a select with no cases never returns, so the call is the only way through.
func afterSelect() {
	afterSelect()
	select {}
}

// Not found: the call follows a select with no cases, and is never reached.
func unreachable() {
	select {}
	unreachable()
}

// Not found: every case calls, but no one call is on every way out.
func everyCase(choice int) {
	switch choice {
	case 0:
		everyCase(1)
	default:
		everyCase(0)
	}
}

// Not found: a switch with a case that returns.
func oneCaseReturns(choice int) {
	switch choice {
	case 0:
		return
	default:
		oneCaseReturns(0)
	}
}
