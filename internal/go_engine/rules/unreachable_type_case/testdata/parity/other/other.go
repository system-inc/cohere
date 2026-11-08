// Package other holds an interface with an unexported method, which no type outside this package implements, and a
// type that implements it.
package other

type Sealed interface {
	Seal()
	sealed()
}

type Implementation struct{}

func (Implementation) Seal() {}

func (Implementation) sealed() {}
