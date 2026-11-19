// Package abstract declares an interface, whose method is a function object of the package with no function in any
// program, for the test of the answer about a function the package holds no body for.
package abstract

// Stopper's Stop has no body anywhere.
type Stopper interface {
	Stop() error
}
