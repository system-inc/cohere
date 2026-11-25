package differences

import "runtime"

// Found by both: the finalizer starts after a directive naming another Go file, which the message names.
func adjusted() {
	object := new(int)
	capture(func() { use(object) })
	runtime.SetFinalizer(object /*line elsewhere.go:100:7*/, func(_ *int) { use(object) })
}
