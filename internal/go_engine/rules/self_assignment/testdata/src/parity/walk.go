package parity

// The port walks the syntax of each top-level function of staticcheck's intermediate representation, where the
// original walks the files: these are the places that walk must still reach, and reach once.

// Found twice: in a function literal of a package-level variable, which the package's initializer holds and which has
// no syntax of its own, and in a literal nested inside it.
var packageLiteral = func() {
	value := 1
	value = value
	nested := func() {
		other := 2
		other = other
		_ = other
	}
	nested()
	_ = value
}

// Found: a blank-named function, which the representation builds though nothing can call it.
func _() {
	value := 1
	value = value
	_ = value
}

// Found once: a literal inside a declared function, judged in the declared function's walk and not again in its own.
func withLiteral() {
	run := func() {
		value := 1
		value = value
		_ = value
	}
	run()
}

// Found: a method's body.
func (receiver *record) method() {
	receiver.field = receiver.field
}

// Found once: a generic function's body, built once and never per instantiation.
func genericBody[T any](value T) {
	value = value
	_ = value
}

// Found once each, the generic function called with two types.
func instantiations() {
	genericBody(1)
	genericBody("text")
}
