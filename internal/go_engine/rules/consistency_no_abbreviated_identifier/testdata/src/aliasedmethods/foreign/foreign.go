// Package foreign stands in for a package we do not own, which aliasedmethods imports and the test never analyzes.
// Cmd is declared by no named type here or anywhere: only an alias names the interface that carries it.
package foreign

// Commanding is an alias of an interface written in place, so it declares no named interface of its own.
type Commanding = interface {
	Cmd() string
}
