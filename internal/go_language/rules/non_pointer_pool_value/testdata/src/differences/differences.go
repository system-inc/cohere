// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, the receiver left out of the
// arguments, f(g()) where g returns the value, and Put held in a variable or as a method expression, whose callee is
// known only once the variable is lifted, or not at all when two functions meet. The port and the original find the
// same here, so the fixture records no difference.
package differences

import "sync"

var pool sync.Pool

func value() []byte {
	return nil
}

func statements(list []int) {
	defer pool.Put(list)
	go pool.Put(1)
	defer func() {
		pool.Put([]byte{})
	}()
}

func multipleValues() {
	pool.Put(value())
}

func lifted(condition bool, list []int) {
	put := pool.Put
	if condition {
		put = pool.Put
	}
	put(list)
	choose := (*sync.Pool).Put
	if condition {
		choose = func(*sync.Pool, any) {}
	}
	choose(&pool, list)
	expression := (*sync.Pool).Put
	expression(&pool, list)
}
