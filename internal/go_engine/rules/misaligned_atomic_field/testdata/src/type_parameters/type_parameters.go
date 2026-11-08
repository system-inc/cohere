// Package type_parameters holds the one place the port and the original differ: in a generic function's own body, the
// address of a field placed after one whose size a type parameter decides, directly, in an array, or in a struct.
// Such a field has no offset until the function is instantiated, and on a 32-bit platform go/types panics when the
// original asks for one, so the original cannot run here at all. The port judges nothing at those addresses, and
// still finds the misaligned field of a struct no type parameter sizes, given in the same bodies, recorded on its line.
package type_parameters

import "sync/atomic"

type direct[Value any] struct {
	head  Value
	count int64
}

type inArray[Value any] struct {
	head  [2]Value
	count int64
}

type inStruct[Value any] struct {
	head  struct{ value Value }
	count int64
}

type viaInstance[Value any] struct {
	head  direct[Value]
	count int64
}

type counters struct {
	first  int64
	flag   int32
	second int64
}

func generic[Value any](
	first *direct[Value],
	second *inArray[Value],
	third *inStruct[Value],
	fourth *viaInstance[Value],
	plain *counters,
) {
	atomic.AddInt64(&first.count, 1)
	atomic.AddInt64(&second.count, 1)
	atomic.AddInt64(&third.count, 1)
	atomic.AddInt64(&fourth.count, 1)
	atomic.AddInt64(&plain.second, 1) // port finds: address of non 64-bit aligned field second passed to sync/atomic.AddInt64
}
