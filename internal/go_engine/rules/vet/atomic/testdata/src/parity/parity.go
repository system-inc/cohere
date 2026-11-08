// Package parity holds every branch of atomic's judgment, each beside a clean case, and the shapes most likely to
// part the original and the port. Each finding is a want, checked against the original, and every line without one
// is clean, its comment saying why.
package parity

import (
	"sync/atomic"

	imitation "imitation/sync/atomic"
)

// Every one of the five Add functions is judged.
func everyAddFunction() {
	var signed32 int32
	var signed64 int64
	var unsigned32 uint32
	var unsigned64 uint64
	var address uintptr
	signed32 = atomic.AddInt32(&signed32, 1)      // want "direct assignment to atomic value"
	signed64 = atomic.AddInt64(&signed64, 1)      // want "direct assignment to atomic value"
	unsigned32 = atomic.AddUint32(&unsigned32, 1) // want "direct assignment to atomic value"
	unsigned64 = atomic.AddUint64(&unsigned64, 1) // want "direct assignment to atomic value"
	address = atomic.AddUintptr(&address, 1)      // want "direct assignment to atomic value"
}

// The rest of sync/atomic is not judged, nor is an Add function that is not sync/atomic's.
func otherFunctions() {
	var signed32 int32
	var signed64 int64
	var unsigned64 uint64
	signed32 = atomic.AndInt32(&signed32, 1)         // clean, And is not an Add
	signed64 = atomic.OrInt64(&signed64, 1)          // clean, Or is not an Add
	signed64 = atomic.SwapInt64(&signed64, 1)        // clean, Swap is not an Add
	signed64 = atomic.LoadInt64(&signed64)           // clean, Load is not an Add
	unsigned64 = imitation.AddUint64(&unsigned64, 1) // clean, not sync/atomic's
	var counter atomic.Int64
	signed64 = counter.Add(1)                   // clean, a method
	signed64 = (*atomic.Int64).Add(&counter, 1) // clean, a method expression
	add := atomic.AddInt64
	signed64 = add(&signed64, 1)               // clean, a function value names no function
	signed64 = (atomic.AddInt64)(&signed64, 1) // the parentheses still name it // want "direct assignment to atomic value"
	_, _ = signed32, unsigned64
}

// The address taken against the variable assigned, compared as gofmt prints them.
func addressOf(other int64) {
	var count int64
	count = atomic.AddInt64(&count, 1)            // want "direct assignment to atomic value"
	count += atomic.AddInt64(&count, 1)           // any assignment token but a lone := // want "direct assignment to atomic value"
	count = atomic.AddInt64(&other, 1)            // clean, another variable
	count = atomic.AddInt64(&count /* note */, 1) // comments are not printed // want "direct assignment to atomic value"
	(count) = atomic.AddInt64(&count, 1)          // clean, "(count)" is not "count"
	count = atomic.AddInt64(&(count), 1)          // clean, "count" is not "(count)"
	count = atomic.AddInt64((&count), 1)          // clean, the address is in parentheses
	pointer := &count
	count = atomic.AddInt64(pointer, 1)      // clean, an address that is not taken there
	*pointer = atomic.AddInt64(&*pointer, 1) // "*pointer" is "*pointer" // want "direct assignment to atomic value"
	_ = other
}

// A dereference assigned against the pointer added to.
func dereference(pointer *int64, other *int64) {
	*pointer = atomic.AddInt64(pointer, 1)   // want "direct assignment to atomic value"
	*pointer = atomic.AddInt64(other, 1)     // clean, another pointer
	*pointer = atomic.AddInt64((pointer), 1) // clean, "pointer" is not "(pointer)"
	(*pointer) = atomic.AddInt64(pointer, 1) // clean, a parenthesized left side is not a dereference
	pointers := []*int64{pointer, other}
	*pointers[0] = atomic.AddInt64(pointers[0], 1) // want "direct assignment to atomic value"
	*pointers[1] = atomic.AddInt64(pointers[0], 1) // clean, another index
}

// Several sides at once, each paired with its own call.
func pairs() {
	var first, second int64
	first, second = atomic.AddInt64(&first, 1), atomic.AddInt64(&second, 1) // want "direct assignment to atomic value" "direct assignment to atomic value"
	first, second = atomic.AddInt64(&second, 1), atomic.AddInt64(&first, 1) // clean, crossed
	first, fresh := atomic.AddInt64(&first, 1), 0                           // := assigns first // want "direct assignment to atomic value"
	single := atomic.AddInt64(&first, 1)                                    // clean, a lone := declares
	pair := func() (int64, int64) { return 0, 0 }
	first, second = pair() // clean, no call per side
	_, _ = fresh, single
}

// Statements that are not assignments are not judged.
func notAssignments() int64 {
	var count int64
	var declared = atomic.AddInt64(&count, 1) // clean, a declaration
	atomic.AddInt64(&count, 1)                // clean, a statement
	_ = declared
	return atomic.AddInt64(&count, 1) // clean, a return
}

type inner struct{ count int64 }

type outer struct {
	inner
	other int64
}

// Fields, embedded ones among them, compared as written.
func fields(holder *outer) {
	holder.count = atomic.AddInt64(&holder.count, 1)       // want "direct assignment to atomic value"
	holder.count = atomic.AddInt64(&holder.inner.count, 1) // clean, written differently
	holder.other = atomic.AddInt64(&holder.count, 1)       // clean, another field
	holder.count = atomic.AddInt64(&holder.
		count, 1) // clean, printed across two lines
}

// Index expressions compare their index as text, so a call in it matches itself.
func indexes(values []int64, next func() int) {
	values[next()] = atomic.AddInt64(&values[next()], 1) // want "direct assignment to atomic value"
	values[0] = atomic.AddInt64(&values[1], 1)           // clean
}

// Closures, generic functions and methods are walked like any other body.
func closure() func() {
	var count int64
	return func() {
		count = atomic.AddInt64(&count, 1) // want "direct assignment to atomic value"
	}
}

func generic[T any](count *int64, _ T) {
	*count = atomic.AddInt64(count, 1) // want "direct assignment to atomic value"
}

type holder[T any] struct{ count int64 }

func (value *holder[T]) increment() {
	value.count = atomic.AddInt64(&value.count, 1) // want "direct assignment to atomic value"
}

// A local named atomic hides the package, so its AddUint64 is not sync/atomic's.
type shadow struct{}

func (shadow) AddUint64(address *uint64, delta uint64) uint64 { return 0 }

func shadowed() {
	var count uint64
	{
		var atomic shadow
		count = atomic.AddUint64(&count, 1) // clean, a method of shadow
	}
	count = atomic.AddUint64(&count, 1) // want "direct assignment to atomic value"
}
