// Package parity holds the shapes SA4017 depends on: which calls the purity pass judges free of side effects, which
// calls staticcheck's form gives no static callee, where a result counts as read, and which functions the benchmark
// exception spares. Each function's comment says what the original makes of it; the parity test holds only that the
// port makes the same.
package parity

import (
	"fmt"
	"strings"
	"testing"
)

type record struct {
	value int
}

func (receiver record) sum(other int) int { return receiver.value + other }

func add(first int, second int) int { return first + second }

func pair(first int, second int) (int, int) { return first + second, first - second }

func noisy(value int) int {
	println(value)
	return value
}

func identity[T any](value T) T { return value }

func stub() int { return 0 }

// Found in each: a local pure function, one of two results, a pure function of the standard library, fmt.Sprintf
// outside the fmt package's tests.
func discarded() {
	add(1, 2)
	pair(1, 2)
	strings.ToUpper("text")
	fmt.Sprintf("%d", 1)
}

// Not found: a result read, a result assigned to the blank identifier, one of two results kept, a call with side
// effects, a stub the purity pass does not trust.
func read() int {
	total := add(1, 2)
	_ = add(3, 4)
	_, kept := pair(1, 2)
	noisy(1)
	stub()
	return total + kept
}

// Not found: a deferred call and a goroutine are no calls whose results are discarded by the form.
func deferredAndConcurrent() {
	defer add(1, 2)
	go add(3, 4)
}

// Not found: a type parameter's method has no static callee.
func throughTypeParameter[T fmt.Stringer](value T) {
	value.String()
}

// Found: a method value bound and then called is built as a call of the method's bound wrapper, a static callee whose
// object is the method, so the finding names the method.
func methodValue() {
	bound := record{}.sum
	bound(1)
}

// Not found: an interface method, a function value and a closure have no static callee or no object.
func dynamic(function func(int, int) int, stringer fmt.Stringer) {
	stringer.String()
	function(1, 2)
	func() int { return 1 }()
}

// Found: a pure method called directly.
func method(local record) {
	local.sum(1)
}

// Not found: a parameter of a type parameter's type is no basic type, so the purity pass judges no generic function
// pure, called with its type argument inferred or given.
func generic() {
	identity(1)
	identity[string]("text")
}

// Found: a function taking a *testing.B outside a test file is no benchmark to the original.
func benchmarkOutsideTests(benchmark *testing.B) {
	add(1, 2)
	_ = benchmark
}

// Found: inside a closure, judged as a function of its own.
func closure() func() {
	return func() {
		add(1, 2)
	}
}

// Found: in a package-level initializer's closure.
var initializer = func() {
	strings.Repeat("a", 2)
}
