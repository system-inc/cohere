// Package parity holds values of each kind of type given to sync.Pool's Put: pointers, slices, maps, channels,
// functions, interfaces, unsafe.Pointer, structs, arrays, strings and numbers, named and not, type parameters whose
// type sets hold a pointer and do not, and the shapes staticcheck's representation builds the call in more than one
// way.
package parity

import (
	"bytes"
	"sync"
	"unsafe"
)

type buffer []byte

type record struct{ value int }

type recordPointer *record

type table map[string]int

type callback func()

type empty struct{}

type putter interface{ Put(any) }

var pool = sync.Pool{New: func() any { return new(record) }}

var pointer = &sync.Pool{}

func init() {
	pool.Put(record{})
}

func pointerLike(value *record, mapped table, channel chan int, function callback, address unsafe.Pointer) {
	pool.Put(value)
	pool.Put(&record{})
	pool.Put(new(int))
	pool.Put(mapped)
	pool.Put(map[int]int{})
	pool.Put(channel)
	pool.Put(function)
	pool.Put(func() {})
	pool.Put(address)
	pool.Put(recordPointer(value))
	pool.Put(new(bytes.Buffer))
	pool.Put(nil)
	var anything any = record{}
	pool.Put(anything)
	var describer interface{ String() string }
	pool.Put(describer)
}

func notPointerLike(list []int, named buffer, value record, array [4]byte, text string) {
	pool.Put(list)
	pool.Put(named)
	pool.Put([]byte{})
	pool.Put(make([]*record, 0))
	pool.Put(value)
	pool.Put(empty{})
	pool.Put(struct{}{})
	pool.Put(array)
	pool.Put(text)
	pool.Put("constant")
	pool.Put(1)
	pool.Put(1.5)
	pool.Put(true)
	pool.Put(uintptr(0))
	pool.Put(bytes.Buffer{})
	pool.Put(*new(record))
	pool.Put(list[0])
}

func receivers(local sync.Pool, pointerToPool *sync.Pool, holder struct{ pool sync.Pool }) {
	local.Put(1)
	pointerToPool.Put(1)
	pointer.Put(1)
	holder.pool.Put(1)
	(&local).Put(1)
	pools := []sync.Pool{{}}
	pools[0].Put(1)
	var other putter = pointerToPool
	other.Put(1)
}

func phis(condition bool, value *record, list []int) {
	var chosen any = value
	if condition {
		chosen = list
	}
	pool.Put(chosen)
	kept := list
	if condition {
		kept = nil
	}
	pool.Put(kept)
}

func callShapes(list []int) {
	defer pool.Put(list)
	go pool.Put(1)
	func() {
		pool.Put(record{})
	}()
	put := pool.Put
	put(list)
	method := (*sync.Pool).Put
	method(&pool, list)
	pool.Put(
		list,
	)
	pool.Put(pool.Get())
}

func generic[Any any, Pointer *record | *int, Slice ~[]int, Number ~int, Mixed ~int | *int](
	anything Any, pointerValue Pointer, list Slice, number Number, mixed Mixed,
) {
	pool.Put(anything)
	pool.Put(pointerValue)
	pool.Put(list)
	pool.Put(number)
	pool.Put(mixed)
	pool.Put(&anything)
}

func instantiated() {
	generic[int, *int, []int, int, int](1, nil, nil, 1, 1)
}
