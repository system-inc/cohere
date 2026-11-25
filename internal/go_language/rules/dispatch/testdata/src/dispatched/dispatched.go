// Package dispatched holds a function, a method and a closure, with calls among them, for the dispatcher to walk.
package dispatched

type counter struct{ count int }

func first() int { // want `first in dispatched, line 6`
	return 1
}

func (value *counter) increment() { // want `increment in dispatched, line 10`
	value.count++
}

func second() func() int { // want `second in dispatched, line 14`
	return func() int { // want `second\$1 in dispatched, line 15`
		return first()
	}
}
