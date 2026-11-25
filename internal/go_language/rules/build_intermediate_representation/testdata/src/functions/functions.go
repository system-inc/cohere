// Package functions holds each place a source function comes from: package functions and methods, two declared
// initializers and the synthetic one, a global initialized by a closure, closures nested in closures, a generic
// function's closure, a deferred closure, and a range over a function, whose loop body is built as a function too.
package functions

import "strings"

type counter struct{ count int }

var greeting = func() string {
	return strings.ToUpper("hello")
}()

var shout = func(word string) string {
	return func() string { return word + "!" }()
}

func init() {
	greeting += "."
}

func first() int {
	return 1
}

func (value *counter) increment() func() {
	return func() {
		value.count++
		func() {
			value.count += first()
		}()
	}
}

func init() {
	greeting = shout(greeting)
}

func mapped[Element any](elements []Element, change func(Element) Element) []Element {
	changed := make([]Element, 0, len(elements))
	for _, element := range elements {
		changed = append(changed, func() Element { return change(element) }())
	}
	return changed
}

func counted() (count int) {
	defer func() {
		count++
	}()
	return first()
}

func pairs(yield func(int, string) bool) {
	yield(1, "one")
}

func ranged() int {
	total := 0
	for number, word := range pairs {
		total += number + len(word)
		if total > 10 {
			break
		}
	}
	return total
}

// caller reaches callee, declared after it, through a static call, as a reader following StaticCallee would.
func caller() int {
	return callee() + 1
}

func callee() int {
	return first() * 2
}
