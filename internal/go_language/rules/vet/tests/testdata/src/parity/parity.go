// Package parity holds what parity's examples name, and functions shaped like tests in a file that is not a test
// file, which neither side judges.
package parity

import "testing"

// Buffer has a field, a promoted method, a pointer method and a value method.
type Buffer struct {
	Size int
	inner
}

type inner struct{}

func (inner) Promoted() {}

func (*Buffer) Grow(int) {}

func (Buffer) Len() int { return 0 }

// List is generic, with a field and a method.
type List[T any] struct {
	Head T
}

func (*List[T]) Push(T) {}

// Map is a generic function.
func Map[K comparable, V any](values map[K]V) []K { return nil }

// Alias names Buffer.
type Alias = Buffer

// Pair is a generic alias.
type Pair[T any] = List[T]

// Shape is an interface with one method.
type Shape interface {
	Area() float64
}

// Default is a variable of a type with methods.
var Default Buffer

// Limit is a constant.
const Limit = 3

// Celsius is a defined type with a method.
type Celsius float64

func (Celsius) String() string { return "" }

func helper() {}

// Not a test file, so none of these is judged.

func Testnontest(t *testing.T) {}

func ExampleMissing(value int) int { return value }

func Fuzznontest(f *testing.F) {
	f.Fuzz(0)
}
