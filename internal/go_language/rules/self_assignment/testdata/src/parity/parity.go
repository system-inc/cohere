// Package parity holds the shapes SA4018 depends on: which assignments it judges, which sides the side effects
// judgment spares, with the purity pass deciding calls, and how the two sides must print alike. Each function's
// comment says what the original makes of it; the parity test holds only that the port makes the same.
package parity

import (
	"os"
	"strings"
)

type record struct {
	field  int
	values []int
	next   *record
}

func (receiver record) pureMethod() int { return receiver.field }

func double(value int) int { return value * 2 }

func noisy(value int) int {
	println(value)
	return value
}

func generic[T any](value T) T { return value }

// Found in each: a variable, a field, a field through a pointer, a dereference, and an element at a constant index.
func plain(pointer *record, local record) {
	number := 1
	number = number
	local.field = local.field
	pointer.next.field = pointer.next.field
	*pointer = *pointer
	local.values[0] = local.values[0]
	_ = number
}

// Found twice in one statement: the pairs of a tuple assignment are judged one by one; a swap is no self-assignment.
func tuple() {
	first, second, third := 1, 2, 3
	first, second, third = first, second, 4
	first, second = second, first
	_, _, _ = first, second, third
}

// Not found: a definition, an operator assignment, and an assignment of a call's results are no plain pairs.
func otherTokens() {
	value := 1
	value += value
	other, rest := split()
	_, _, _ = value, other, rest
}

func split() (int, int) { return 1, 2 }

// Not found: one side parenthesized is another kind of node; found: both sides parenthesized print alike.
func parentheses() {
	value := 1
	(value) = value
	(value) = (value)
	_ = value
}

// Not found: one side broken across lines, which the printer keeps from the file's positions, so the two print apart.
func lineBreak(values []int) {
	values[double(
		1)] = values[double(1)]
}

// Found where the index calls only functions the purity pass holds, or len and cap; not found where a call is
// impure, a method or a generic function the pass does not hold, a conversion, a receive, or an allocation.
func indices(values []int, local record, channel chan int, table map[*int]int, text string) {
	values[double(1)] = values[double(1)]
	values[local.pureMethod()] = values[local.pureMethod()]
	values[len(values)] = values[len(values)]
	values[cap(values)] = values[cap(values)]
	values[noisy(1)] = values[noisy(1)]
	values[int(int8(1))] = values[int(int8(1))]
	values[<-channel] = values[<-channel]
	table[new(int)] = table[new(int)]
	values[len(strings.ToUpper(text))] = values[len(strings.ToUpper(text))]
	values[generic(1)] = values[generic(1)]
}

// Found: a package's variable assigned to itself.
func packageVariable() {
	os.Args = os.Args
}

// Found: a function value and a map's element at a constant key.
func valuesOfOtherKinds(function func(), table map[string]int) {
	function = function
	table["key"] = table["key"]
	_ = function
}

// Found: in a closure, and in a generic function; not found: a closure's definition that shadows.
func nested() {
	value := 1
	func() {
		value = value
		value := value
		_ = value
	}()
}

func genericFunction[T any](value T) {
	value = value
	_ = value
}

// Found: comments inside the sides are not printed, so they do not tell the sides apart.
func comments(values []int) {
	values[ /* first */ 0] = values[0]
}
