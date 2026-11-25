// Package parity holds the shapes staticcheck's representation builds a field's address or a call in more than one
// way, each of sync/atomic's 64-bit functions given a field that is aligned on a 32-bit platform and one that is not,
// and the addresses the check does not judge: an element of an array or a slice, a variable, and two fields that
// meet. It carries no build constraint, since the tests run it on 32-bit and 64-bit platforms alike.
package parity

import (
	"sync/atomic"
	"unsafe"
)

type counters struct {
	first  int64
	flag   int32
	second int64
	third  uint64
	small  int8
	fourth uint64
}

type padded struct {
	flag   int32
	_      int32
	second int64
}

type inner struct {
	flag   int32
	nested int64
}

type outer struct {
	first int64
	inner inner
}

type promoted struct {
	flag int32
	inner
}

type named *counters

type pair[Value any] struct {
	head  Value
	count int64
}

type tail[Value any] struct {
	first  int64
	flag   int32
	second int64
	rest   Value
}

type pointerFirst struct {
	pointer unsafe.Pointer
	count   int64
}

func eachFunction() {
	var value counters
	atomic.AddInt64(&value.first, 1)
	atomic.AddInt64(&value.second, 1)
	atomic.AddUint64(&value.third, 1)
	atomic.AddUint64(&value.fourth, 1)
	atomic.CompareAndSwapInt64(&value.first, 0, 1)
	atomic.CompareAndSwapInt64(&value.second, 0, 1)
	atomic.CompareAndSwapUint64(&value.third, 0, 1)
	atomic.CompareAndSwapUint64(&value.fourth, 0, 1)
	atomic.LoadInt64(&value.first)
	atomic.LoadInt64(&value.second)
	atomic.LoadUint64(&value.third)
	atomic.LoadUint64(&value.fourth)
	atomic.StoreInt64(&value.first, 1)
	atomic.StoreInt64(&value.second, 1)
	atomic.StoreUint64(&value.third, 1)
	atomic.StoreUint64(&value.fourth, 1)
	atomic.SwapInt64(&value.first, 1)
	atomic.SwapInt64(&value.second, 1)
	atomic.SwapUint64(&value.third, 1)
	atomic.SwapUint64(&value.fourth, 1)
}

func throughPointers(value *counters, alias named, wrapped *padded) {
	atomic.AddInt64(&value.second, 1)
	atomic.AddInt64(&alias.second, 1)
	atomic.AddInt64(&alias.first, 1)
	atomic.AddInt64(&wrapped.second, 1)
}

func nested(value *outer, through *promoted) {
	atomic.AddInt64(&value.first, 1)
	atomic.AddInt64(&value.inner.nested, 1)
	atomic.AddInt64(&through.nested, 1)
	atomic.AddInt64(&through.inner.nested, 1)
}

func elements(values []counters, array *[4]int64, slice []int64) {
	atomic.AddInt64(&values[1].second, 1)
	atomic.AddInt64(&values[1].first, 1)
	atomic.AddInt64(&array[1], 1)
	atomic.AddInt64(&slice[3], 1)
}

func variables() {
	var word int64
	atomic.AddInt64(&word, 1)
	held := new(int64)
	atomic.AddInt64(held, 1)
}

func meeting(value *counters, condition bool) {
	address := &value.second
	if condition {
		address = &value.second
	}
	atomic.AddInt64(address, 1)
	other := &value.second
	if condition {
		other = &value.first
	}
	atomic.AddInt64(other, 1)
}

func generic[Value any](word *pair[int32], wide *pair[int64], after *tail[Value]) {
	atomic.AddInt64(&word.count, 1)
	atomic.AddInt64(&wide.count, 1)
	atomic.AddInt64(&after.first, 1)
	atomic.AddInt64(&after.second, 1)
}

func pointers(value *pointerFirst) {
	atomic.AddInt64(&value.count, 1)
}

func closures(value *counters) {
	add := func() {
		atomic.AddInt64(&value.second, 1)
	}
	add()
}

var global counters

func init() {
	atomic.AddInt64(&global.second, 1)
	atomic.AddInt64(&global.first, 1)
}
