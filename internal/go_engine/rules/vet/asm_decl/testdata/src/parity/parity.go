// Package parity declares, without bodies, the functions its assembly files implement, one for each shape of
// argument frame the pass lays out: every size of value, every composite it splits into words, unnamed parameters and
// results, named results, variadic parameters, aliases, named types, a method and a method of a generic type.
package parity

import "unsafe"

type pair struct {
	low  int8
	wide int64
}

type nested struct {
	name  string
	inner pair
	flags [3]bool
}

type celsius float64

type alias = int32

type reader interface {
	read()
}

type receiver struct{}

type box[T any] struct{}

func bytes(x int8, y uint8, z bool)
func words(x int16, y uint16)
func longs(x int32, y uint32, f float32)
func quads(x int64, y uint64, d float64, p uintptr)
func machine(x int, y uint)
func pointers(p *int, c chan int, m map[string]int, f func(), u unsafe.Pointer)
func text(s string) (n int)
func slices(s []int, t []string)
func interfaces(e any, r reader, i interface{})
func complexes(c complex64, d complex128)
func structs(p pair, n nested)
func arrays(a [2]int16, b [2][3]uint8, e [0]int, after int8)
func named(c celsius, a alias)
func variadic(prefix string, rest ...int)
func unnamed(int8, int64) (int32, string)
func unnamedResult(x int) int
func unnamedResults(x int) (int, int)
func namedResults(x int) (sum int, ok bool)
func returnsString() string
func returnsStruct() pair
func returnsComplex() complex128
func nothing()
func noFrame(x int32) int32
func colliding(ret int) int
func stackPointer(x int64, y int64) int64
func dataFollows(x int64) int64
func abiInternal() int
func abiInternalMissing() int
func abiInternalInstruction() int
func abiZero(x int) int
func unicode(名前 int64, héllo int64)
func wasmImport() int

func (receiver) method(x int)

// A generic function cannot be declared without a body, which go/types refuses ("generic function is missing
// function body"), so it cannot be a fixture. A method of a generic type can, since go/types refuses only a
// declaration with type parameters of its own, and the pass lays out no receiver, so the method's frame is its
// parameters'. One holding a type parameter by value has a frame go/types cannot measure, where the original crashes,
// so it is in the differences fixture; one holding it behind a pointer, a slice, a map, a function or a channel
// measures as words, and both judge it the same.
func (box[T]) generic(x int64, s string)
func (box[T]) measured(p *T, l []T, m map[string]T, f func(T) T, c chan T)
