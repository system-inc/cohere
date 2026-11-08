// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the
// address among several values, and a function of sync/atomic held in a variable, whose callee is known only once the
// variable is lifted, or not at all when two functions meet. The port and the original find the same here, so the
// fixture records no difference. It carries no build constraint, and the tests run it on a 32-bit platform.
package differences

import "sync/atomic"

type counters struct {
	first  int64
	flag   int32
	second int64
}

func addressAndDelta(value *counters) (*int64, int64) {
	return &value.second, 1
}

func statements(value *counters) {
	defer atomic.AddInt64(&value.second, 1)
	go atomic.StoreInt64(&value.second, 1)
	defer func() {
		atomic.LoadInt64(&value.second)
	}()
}

func multipleValues(value *counters) {
	atomic.AddInt64(addressAndDelta(value))
}

func lifted(value *counters, condition bool) {
	add := atomic.AddInt64
	if condition {
		add = atomic.AddInt64
	}
	add(&value.second, 1)
	choose := atomic.AddInt64
	if condition {
		choose = func(address *int64, delta int64) int64 { return 0 }
	}
	choose(&value.second, 1)
}
