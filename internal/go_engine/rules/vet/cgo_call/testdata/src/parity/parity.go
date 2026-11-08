// Package parity holds every branch of cgocall's judgment, a violation of each kind beside a clean case, and the
// shapes most likely to differ between the original and the port. The cgo tool translates it, so both judge its cgo
// files as written through the translation's `//line` directives. A line that ends in `finds` is one both report;
// every other call is clean.
package parity

// static void take(void *pointer) {}
// static void takeTwo(void *first, void *second) {}
// static int number(int value) { return value; }
import "C"

import (
	"fmt"
	"unsafe"
)

// Kinds hands C the address of a value of every kind the rules forbid, and of kinds they allow, converted to
// unsafe.Pointer.
func Kinds() {
	var channel chan int
	C.take(unsafe.Pointer(&channel)) // finds
	var table map[int]int
	C.take(unsafe.Pointer(&table)) // finds
	var function func()
	C.take(unsafe.Pointer(&function)) // finds
	var values []int
	C.take(unsafe.Pointer(&values)) // finds
	var pointerToSlice *[]int
	C.take(unsafe.Pointer(&pointerToSlice)) // finds
	C.take(unsafe.Pointer(pointerToSlice))  // finds
	var arrayOfMaps [2]map[int]int
	C.take(unsafe.Pointer(&arrayOfMaps)) // finds
	var nested struct{ inner struct{ channel chan int } }
	C.take(unsafe.Pointer(&nested)) // finds
	var callback struct{ call func() }
	C.take(unsafe.Pointer(&callback))  // finds
	C.take(unsafe.Pointer(&[]int{1}))  // finds
	C.take(unsafe.Pointer(new([]int))) // finds

	var number int
	C.take(unsafe.Pointer(&number))
	var pointers [2]*int
	C.take(unsafe.Pointer(&pointers))
	var face any
	C.take(unsafe.Pointer(&face))
	var text string
	C.take(unsafe.Pointer(&text))
	var flat struct{ count int }
	C.take(unsafe.Pointer(&flat))
	C.take(unsafe.Pointer(&values[0]))
	var pointer unsafe.Pointer
	C.take(pointer)
	C.take(nil)
	_ = C.number(C.int(len(values)))
	C.CBytes([]byte("text"))
	_ = C.GoBytes(unsafe.Pointer(&values), 1) // finds
}

// Named hands C values of the package's named types, declared in plain.go, whose names the file type-checked again
// reaches through its import of the package checked: exported ones resolve, and unexported ones do not, so they
// are never judged.
func Named() {
	var embedding Embedding
	C.take(unsafe.Pointer(&embedding)) // finds
	var tree Tree
	C.take(unsafe.Pointer(&tree)) // finds
	var list List
	C.take(unsafe.Pointer(&list))
	var channel Channel
	C.take(unsafe.Pointer(&channel)) // finds
	var aliased SliceAlias
	C.take(unsafe.Pointer(&aliased)) // finds
	var boxed Box[map[int]int]
	C.take(unsafe.Pointer(&boxed)) // finds
	var boxedNumber Box[int]
	C.take(unsafe.Pointer(&boxedNumber))
	var hidden hiddenHolder
	C.take(unsafe.Pointer(&hidden))
	C.take(unsafe.Pointer(&Shared)) // finds
	C.take(unsafe.Pointer(&hiddenShared))
	C.take(unsafe.Pointer(&Fixed))
}

// Conversions spells the conversion to unsafe.Pointer, and the read of one through a pointer, every way the judgment
// looks through or stops at.
func Conversions() {
	var channel chan int
	C.take(*(*unsafe.Pointer)(unsafe.Pointer(&channel)))  // finds
	C.take(*(PointerToPointer)(unsafe.Pointer(&channel))) // finds
	C.take(*(*PointerAlias)(PointerAlias(&channel)))      // finds
	C.take(unsafe.Pointer(Defined(&channel)))             // finds
	C.take(PointerAlias(&channel))                        // finds
	C.take(unsafe.Pointer((&channel)))                    // finds
	C.take((unsafe.Pointer(&channel)))
	C.take(*(*unsafe.Pointer)(unsafe.Pointer(unsafe.Pointer(&channel))))

	// Reads of an unsafe.Pointer the judgment stops at, and judges by the type read.
	_ = C.number(*(*C.int)(unsafe.Pointer(&channel)))
	var pointerToPointer *unsafe.Pointer
	C.take(*pointerToPointer)
	C.take(*(*unsafe.Pointer)(pointerToPointer))
	address := func(any) unsafe.Pointer { return nil }
	C.take(*(*unsafe.Pointer)(address(&channel)))
	var pointerToSlice *[]int
	C.take(*(*unsafe.Pointer)(unsafe.Pointer(pointerToSlice)))
	pointerOf := func() *unsafe.Pointer { return nil }
	C.take(*pointerOf())
	convert := func(unsafe.Pointer) *unsafe.Pointer { return nil }
	C.take(*convert(unsafe.Pointer(&channel)))
	C.take(*(*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(&channel), 0)))
}

// Arguments hands C more than one argument, each judged in turn until one is reported, and calls that are not C's.
func Arguments() {
	var values []int
	var table map[int]int
	var text string
	C.takeTwo(unsafe.Pointer(&text), unsafe.Pointer(&values))  // finds
	C.takeTwo(unsafe.Pointer(&values), unsafe.Pointer(&table)) // finds
	C.takeTwo(unsafe.Pointer(&text), unsafe.Pointer(&text))
	fmt.Println(unsafe.Pointer(&values))
	fmt.Println(C.number(C.int(len(values))))
	holder := struct {
		C struct{ take func(unsafe.Pointer) }
	}{}
	holder.C.take(unsafe.Pointer(&values))
	(C.take)(unsafe.Pointer(&values)) // finds
}

// Generic is a generic function, whose type parameters' underlying types are their constraints, interfaces, so
// values of them are never judged.
func Generic[Value any, Values ~[]int](value Value, values Values) {
	C.take(unsafe.Pointer(&value))
	C.take(unsafe.Pointer(&values))
	var boxed Box[Value]
	C.take(unsafe.Pointer(&boxed))
	var slices Box[[]Value]
	C.take(unsafe.Pointer(&slices)) // finds
	Generic[chan int, []int](nil, nil)
}
