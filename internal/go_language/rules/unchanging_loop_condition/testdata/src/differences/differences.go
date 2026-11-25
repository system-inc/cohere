// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both walk the same syntax and read the same intermediate representation. A loop
// inside a function literal is walked once from every function whose syntax holds it, so a port that judged it from
// the wrong function would report it twice or not at all; a loop the builder never reaches, or reaches only through a
// label, might hold no value for its condition.
package differences

func consume(values ...int) {}

// Found by both, once: a loop three function literals deep, walked from four functions.
func deepLiterals() {
	func() {
		func() {
			func() {
				other := 0
				for index := 0; index < 3; other++ {
				}
			}()
		}()
	}()
}

// Found by both, once each: the enclosing function and the literal inside it hold the same shape of loop.
func sameLoopOutsideAndInside() {
	other := 0
	for index := 0; index < 3; other++ {
	}
	inner := func() {
		other := 0
		for index := 0; index < 3; other++ {
		}
	}
	inner()
}

// Found by both: a literal deferred, and one run as a goroutine.
func deferredAndGoroutine() {
	defer func() {
		other := 0
		for index := 0; index < 3; other++ {
		}
	}()
	go func() {
		other := 0
		for index := 0; index < 3; other++ {
		}
	}()
}

// Found by both: a literal assigned to a package variable, built in the package's init.
var initialized = func() int {
	other := 0
	for index := 0; index < 3; other++ {
	}
	return other
}()

// Found by neither: unreachable after a return, a loop the builder builds no value for.
func afterReturn() {
	return
	other := 0
	for index := 0; index < 3; other++ {
	}
}

// Found by both: inside a branch that is never taken, which the builder still builds.
func neverTaken() {
	if false {
		other := 0
		for index := 0; index < 3; other++ {
		}
	}
}

// Found by both: a labeled loop, reached by a goto as well as by falling into it.
func labeled(limit int) {
	other := 0
	if limit > 2 {
		goto loop
	}
	consume(other)
loop:
	for index := 0; index < limit; other++ {
		if other > limit {
			break loop
		}
	}
}

// Found by both: a generic function's literal, the loop inside it reading the type parameter.
func genericLiteral[T int | int64](limit T) {
	func() {
		var other T
		for index := T(0); index < limit; other++ {
		}
	}()
}
