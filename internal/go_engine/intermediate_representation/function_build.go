package intermediate_representation

// Build builds function's body if it isn't built, and returns once it is. Every caller gets the function's own
// blocks and the anonymous functions its syntax holds, which are built inside the same buildOnce, an anonymous
// function with the function it is declared in.
//
// The synthetic functions the body creates (bound method wrappers, thunks, instantiation wrappers, methods from type
// information) are built by the builder that ran the body, in its later turns. The caller whose builder ran the body
// returns with them built. A second caller returns once the body is built, which can be before them: it never ran
// the body, so it queued none of them and waits for none. So a reader of such a function's blocks calls Build on that
// function first, which builds it or waits for the builder building it.
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
