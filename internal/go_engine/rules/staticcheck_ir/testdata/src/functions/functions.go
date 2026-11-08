// Package functions holds a function, a method and a closure, each of which buildir builds.
package functions

type counter struct{ count int }

func first() int { // want `first in functions, line 6`
	return 1
}

func (value *counter) increment() { // want `increment in functions, line 10`
	value.count++
}

func second() func() int { // want `second in functions, line 14`
	return func() int { // want `second\$1 in functions, line 15`
		return first()
	}
}
