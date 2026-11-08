package differences

import "runtime"

// Found by both: the finalizer starts after a directive naming a grammar, so the message names this file as written.
func grammar() {
	object := new(int)
	capture(func() { use(object) })
	runtime.SetFinalizer(object /*line parser.y:40:3*/, func(_ *int) { use(object) })
}
