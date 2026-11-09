// Package purity holds the shapes purity depends on, each function marked `is pure` where the facts hold it pure and
// unmarked where they do not: basic and struct parameters, results, stubs, calls to the package's own functions in
// either order, to itself, in a cycle, to a closure, through an interface, to builtins and to the standard library,
// listed pure or not, and writes, reads and allocations on the stack and off it. Most are exported, so the facts
// compared across packages cover them; a few are not, which only the facts within the package see.
package purity

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Point is a struct of basic fields, which counts as basic.
type Point struct{ X, Y int }

// Counter is a basic type with methods.
type Counter int

// Holder holds a pointer, so it is no basic type.
type Holder struct{ Value *int }

// Pure: basic parameters, a result, and arithmetic.
func Add(left int, right int) int { // want Add:"is pure"
	return left*2 + right
}

// Pure: a struct of basic fields, read.
func Sum(point Point) int { // want Sum:"is pure"
	return point.X + point.Y
}

// Not pure: a struct holding a pointer is no basic type.
func Held(holder Holder) int {
	return *holder.Value + 1
}

// Not pure: a pointer parameter.
func Dereference(pointer *int) int {
	return *pointer + 1
}

// Not pure: a stub, returning a constant.
func Constant() int {
	return 1
}

// Not pure: a stub too, since returning a parameter takes no instruction but the return.
func Identity(value int) int {
	return value
}

// Not pure: no result.
func NoResult(value int) {
	_ = value + 1
}

// Pure: calls itself.
func Factorial(value int) int { // want Factorial:"is pure"
	if value <= 1 {
		return 1
	}
	return value * Factorial(value-1)
}

// Not pure: a cycle, Even asks Odd, which asks Even while it is still being judged.
func Even(value int) bool {
	if value == 0 {
		return true
	}
	return Odd(value - 1)
}

// Not pure: the other half of the cycle.
func Odd(value int) bool {
	if value == 0 {
		return false
	}
	return Even(value - 1)
}

// Pure: calls a pure function declared after it.
func EarlyCaller(value int) int { // want EarlyCaller:"is pure"
	return LateCallee(value) + 1
}

// Pure: called before it is declared.
func LateCallee(value int) int { // want LateCallee:"is pure"
	return value * 3
}

// Pure: len is allowed.
func Length(text string) int { // want Length:"is pure"
	return len(text) + 1
}

// Not pure: append is no allowed builtin.
func Appended(value int) int {
	values := append([]int(nil), value)
	return values[0]
}

// Pure: strings.ToLower is listed pure.
func Lower(text string) string { // want Lower:"is pure"
	return strings.ToLower(text) + "."
}

// Pure: time.Unix is listed pure, and a method of time.Time on its result too.
func Year(seconds int64) int { // want Year:"is pure"
	return time.Unix(seconds, 0).Year() + 1
}

// Not pure: fmt.Println is not listed, and writes.
func Printed(value int) int {
	fmt.Println(value)
	return value + 1
}

// Not pure: a call through an interface.
func Message(text string) string {
	failure := errors.New(text)
	return failure.Error() + "."
}

// Not pure: a closure has no object, so its call is never pure.
func ThroughClosure(value int) int {
	double := func() int { return value * 2 }
	return double() + 1
}

// Pure: a local struct's fields written and read on the stack.
func LocalField(value int) int { // want LocalField:"is pure"
	var point Point
	point.X = value
	point.Y = value + 1
	return point.X + point.Y
}

// Not pure: taking the struct's address moves it to the heap, though it never leaves the function.
func AddressTaken(value int) int {
	var point Point
	pointer := &point
	pointer.X = value
	return point.X + 1
}

// Not pure: the address escapes, so the variable is on the heap.
func Escaping(value int) *int {
	return &value
}

// Not pure: a panic.
func Panicking(value int) int {
	if value < 0 {
		panic("negative")
	}
	return value + 1
}

// Pure: a deferred call of a pure function.
func Deferred(value int) int { // want Deferred:"is pure"
	defer Add(value, value)
	return value + 1
}

// Not pure: a go statement.
func Started(value int) int {
	go Add(value, value)
	return value + 1
}

// Not pure: a send.
func Sent(value int) int {
	channel := make(chan int, 1)
	channel <- value
	return value + 1
}

// Not pure: a select.
func Selected(value int) int {
	channel := make(chan int, 1)
	select {
	case channel <- value:
	default:
	}
	return value + 1
}

// Pure: a method on a basic type, its receiver a basic parameter.
func (counter Counter) Double() int { // want Double:"is pure"
	return int(counter) * 2
}

// Not pure: a pointer receiver.
func (counter *Counter) Increment() int {
	*counter++
	return int(*counter)
}

// Pure: a method called statically.
func DoubledTwice(value int) int { // want DoubledTwice:"is pure"
	return Counter(value).Double() * 2
}

// Not pure: a type parameter's type is no basic type.
func Maximum[Number int | float64](left Number, right Number) Number {
	if left > right {
		return left
	}
	return right
}

// Through an instance of a generic function, whose wrapper both sides judge alike.
func ThroughGeneric(left int, right int) int {
	return Maximum(left, right) + 1
}

// Pure, and unexported: only the facts within the package see it.
func unexported(value int) int { // want unexported:"is pure"
	return value - 1
}

// Pure: calls the unexported pure function.
func CallsUnexported(value int) int { // want CallsUnexported:"is pure"
	return unexported(value) * 2
}
