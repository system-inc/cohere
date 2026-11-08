// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both walk the same syntax and read the same intermediate representation. A
// function literal's parameters are walked from every function whose syntax holds the literal, so a port that judged
// them from the wrong function would report them twice or not at all; a read or an assignment the builder never
// reaches, or a blank parameter, might be judged differently by the syntax and by the form.
package differences

func consume(values ...int) {}

// Found by both, once: a literal three deep, its parameter overwritten, walked from four functions.
func deepLiterals() {
	func() {
		func() {
			func(value int) {
				value = 1
				consume(value)
			}(0)
		}()
	}()
}

// Found by both, once each: a literal deferred, and one run as a goroutine.
func deferredAndGoroutine() {
	defer func(value int) {
		value = 1
		consume(value)
	}(0)
	go func(value int) {
		value = 2
		consume(value)
	}(0)
}

// Found by both: a literal assigned to a package variable, built in the package's init.
var initialized = func(value int) int {
	value = 3
	return value
}(0)

// Found by both: the only read of the parameter is unreachable, after a return.
func readAfterReturn(value int) {
	value = 4
	consume(value)
	return
	consume(value)
}

// Found by both: the only assignment is unreachable, which the syntax still holds.
func assignedAfterReturn(value int) {
	return
	value = 5
	consume(value)
}

// Found by both: the parameter is read only inside a branch that is never taken.
func readNeverTaken(value int) {
	value = 6
	if false {
		consume(value)
	}
}

// Found by neither: a blank parameter, assigned through the blank identifier.
func blankParameter(_ int) {
	_ = 7
}

// Found by both: a literal's parameter of the same name as the enclosing function's, the outer one read.
func sameNameInside(value int) {
	consume(value)
	func(value int) {
		value = 8
		consume(value)
	}(value)
}

// Found by both: a generic function's literal, its parameter of the type parameter.
func genericLiteral[T any](replacement T) {
	func(value T) {
		value = replacement
		_ = value
	}(replacement)
}

// Found by neither: a type switch reads the parameter before it is overwritten.
func typeSwitchThenAssign(value any) any {
	switch value.(type) {
	}
	value = 9
	return value
}
