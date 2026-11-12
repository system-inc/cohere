// Package foreign is another package's code, which aliasedmethods imports and the test never analyzes. Store is
// declared by no named type here or anywhere: only an alias names the interface that carries it.
package foreign

// Storing is an alias of an interface written in place, so it declares no named interface of its own.
type Storing = interface {
	Store(value any)
}
