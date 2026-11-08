// Package functions holds a function, a method and a closure, each of which buildir builds.
package functions

type counter struct{ count int }

func first() int {
	return 1
}

func (value *counter) increment() {
	value.count++
}

func second() func() int {
	return func() int {
		return first()
	}
}
