// Package parity holds the shapes SA5005 depends on: where the object handed to runtime.SetFinalizer lives, and what
// the finalizer handed with it binds. The IR lifts each load of a variable that comes before the first closure
// capturing it, so the call reads the value stored rather than a load, and the original finds nothing there; most
// functions here capture the object in a closure first, so the call does read a load. Each function's comment says
// what the original makes of it; the parity test holds only that the port makes the same.
package parity

import "runtime"

type resource struct {
	handle int
}

func (target *resource) Close() {}

type holder struct {
	object *resource
}

func use(values ...any) {}

func capture(closure func()) {}

// Not found: the finalizer is the first closure over the object, made after the object's load, which the IR lifts to
// the value stored.
func literalFirst() {
	object := &resource{}
	runtime.SetFinalizer(object, func(_ *resource) {
		use(object)
	})
}

// Found: the closure is held in a variable first.
func throughVariable() {
	object := &resource{}
	capture(func() { use(object) })
	finalizer := func(_ *resource) { use(object) }
	runtime.SetFinalizer(object, finalizer)
}

// Found: a parameter the closure captures is moved to an allocation like any captured variable.
func parameter(object *resource) {
	capture(func() { use(object) })
	runtime.SetFinalizer(object, func(_ *resource) {
		use(object)
	})
}

// Found once: the closure binds the object and another variable.
func twoBindings() {
	object := &resource{}
	capture(func() { use(object) })
	other := &resource{}
	runtime.SetFinalizer(object, func(_ *resource) {
		use(object, other)
	})
}

// Found: a function literal that names the object, once a closure has captured it.
func literal() {
	object := &resource{}
	capture(func() { use(object) })
	runtime.SetFinalizer(object, func(_ *resource) {
		use(object)
	})
}

// Found: a deferred call is a call too.
func deferred() {
	object := &resource{}
	capture(func() { use(object) })
	defer runtime.SetFinalizer(object, func(_ *resource) { use(object) })
}

// Found: and so is a goroutine's.
func spawned() {
	object := &resource{}
	capture(func() { use(object) })
	go runtime.SetFinalizer(object, func(_ *resource) { use(object) })
}

// Found: an object already of an interface type needs no conversion.
func asInterface() {
	var object any = &resource{}
	capture(func() { use(object) })
	runtime.SetFinalizer(object, func(_ any) { use(object) })
}

// Not found: the finalizer uses its own parameter.
func ownParameter() {
	object := &resource{}
	capture(func() { use(object) })
	runtime.SetFinalizer(object, func(finalized *resource) { use(finalized) })
}

// Not found: no finalizer.
func cleared() {
	object := &resource{}
	capture(func() { use(object) })
	runtime.SetFinalizer(object, nil)
}

// Not found: the closure binds another variable.
func otherVariable() {
	object := &resource{}
	capture(func() { use(object) })
	other := &resource{}
	use(object)
	runtime.SetFinalizer(object, func(_ *resource) { use(other) })
}

// Not found: a method value binds the object's value, not the variable it lives in.
func methodValue() {
	object := &resource{}
	runtime.SetFinalizer(object, func(*resource) {})
	use(func() { use(object) })
	runtime.SetFinalizer(object, (*resource).Close)
}

// Not found: the object is a field, loaded through the field's address.
func field(container *holder) {
	runtime.SetFinalizer(container.object, func(_ *resource) { use(container) })
}

// Not found: a named function closes over nothing.
func named() {
	object := &resource{}
	capture(func() { use(object) })
	runtime.SetFinalizer(object, finalize)
}

func finalize(_ *resource) {}

// Not found: inside a closure, the object is the closure's free variable, not an allocation of its own.
func nested() {
	object := &resource{}
	func() {
		runtime.SetFinalizer(object, func(_ *resource) { use(object) })
	}()
}
