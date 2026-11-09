package intermediate_representation

// Build builds function's body if it isn't built, and returns once it is: its blocks, the anonymous functions
// its syntax holds, built with it as the builder makes them, and every synthetic function its body creates (bound
// method wrappers, thunks, instantiation wrappers, methods from type information). An anonymous function is built
// with the function it is declared in.
//
// It is the lazy door to the same build Package.Build runs for every function at once: each function is built
// at most once, under its buildOnce, by whichever of a lazy Build or a package build reaches it first, and the
// result doesn't depend on which, or on what else was built before. Safe to call from many goroutines at once.
//
// The package's own data, its syntax and type information, stays until Package.Build has built everything; a
// function built lazily before that reads it there.
func (function *Function) Build() {
	enclosing := function
	for enclosing.parent != nil {
		enclosing = enclosing.parent
	}
	var functionBuilder builder
	functionBuilder.enqueue(enclosing)
	functionBuilder.iterate()
}
