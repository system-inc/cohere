// Package lookalike holds calls that look like a panic or a Must call and are neither.
package lookalike

import "regexp"

// A local function named panic is not the builtin.
func shadowedPanic() {
	panic := func(message string) {}
	panic("not the builtin")
}

type logger struct{}

func (logger) panic(message string) {}

// A method named panic is the logger's, not the builtin.
func methodNamedPanic() {
	logger{}.panic("logged")
}

func Mustard(name string) string { return name }

func Muster(name string) string { return name }

func mustLoad(name string) string { return name }

func Must_load(name string) string { return name }

// Must followed by a lower-case letter, an underscore, or nothing upper-case is another word.
func otherWords() {
	_ = Mustard("yellow")
	_ = Muster("troops")
	_ = mustLoad("lower")
	_ = Must_load("underscore")
}

// MustType is a type, and calling a type converts a value; it runs no function.
type MustType int

func conversion() {
	_ = MustType(1)
}

// A Must function named but not called panics nowhere here, and a variable holding it is a variable.
func reference() {
	compile := regexp.MustCompile
	_ = compile
}

type options struct {
	MustCompile func(pattern string) string
}

// A field holding a function is a variable, not a function or method.
func field(given options) {
	_ = given.MustCompile("field")
}
