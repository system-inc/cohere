package parity

import (
	. "sync/atomic"
	counting "sync/atomic"
)

// A dot import and a renamed import name sync/atomic's functions as surely as its own name does.
func importedAs() {
	var count int64
	count = AddInt64(&count, 1)          // dot imported // want "direct assignment to atomic value"
	count = counting.AddInt64(&count, 1) // renamed // want "direct assignment to atomic value"
	var unsigned uint32
	unsigned = AddUint32(&unsigned, 1) // dot imported // want "direct assignment to atomic value"
}
