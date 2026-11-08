package parity

// The declarations the file type-checked again changes: methods become functions whose first parameter is the
// receiver, functions and package-level variables and constants are blanked, and types are dropped, each with its
// body or initial value kept and judged.

// static void take(void *pointer) {}
import "C"

import "unsafe"

// Pass hands C its holder's slice, through a receiver the file type-checked again makes a parameter.
func (holder *Holder) Pass() {
	C.take(unsafe.Pointer(&holder.Values)) // finds
	C.take(unsafe.Pointer(&holder.Count))
}

// Value hands C its parameter, beside an unnamed receiver.
func (Holder) Value(values []int, count int) {
	C.take(unsafe.Pointer(&values)) // finds
	C.take(unsafe.Pointer(&count))
}

// Taken is computed by a closure that hands C a slice.
var Taken = func() int {
	var values []int
	C.take(unsafe.Pointer(&values)) // finds
	return 0
}()

var first, second = func() int {
	var table map[int]int
	C.take(unsafe.Pointer(&table)) // finds
	return 0
}(), 1

// Size is a constant the file type-checked again blanks.
const Size = 3

// local is a type the file type-checked again drops.
type local struct{ values []int }

// Closures hands C values from within closures, nested and deferred.
func Closures() {
	var values []int
	func() {
		C.take(unsafe.Pointer(&values)) // finds
		func() {
			var channel chan int
			C.take(unsafe.Pointer(&channel)) // finds
		}()
	}()
	defer C.take(unsafe.Pointer(&values)) // finds
	go C.take(unsafe.Pointer(&values))    // finds
	type inner struct{ table map[int]int }
	var declared inner
	C.take(unsafe.Pointer(&declared)) // finds
	var dropped local
	C.take(unsafe.Pointer(&dropped))
	var size [Size]int
	C.take(unsafe.Pointer(&size))
}
