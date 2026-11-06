package main

// A file of only an interface and its marker variant serves no type with methods, so it has no name to match, though
// Stop has a method.
type Signal interface {
	signalValue()
}

type Stop struct {
	code int
}

func (Stop) signalValue() {}
