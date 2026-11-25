// Package dependency holds wrappers parity calls from another package, so each is found through the fact its
// declaration exports, in every shape a wrapper is declared.
package dependency

import (
	"fmt"
	"log"
)

// Logf is a printf wrapper.
func Logf(format string, arguments ...any) {
	log.Printf(format, arguments...)
}

// Logln is a print wrapper with a parameter before its arguments.
func Logln(level int, arguments ...any) {
	fmt.Println(arguments...)
}

// Failf is an errorf wrapper.
func Failf(format string, arguments ...any) error {
	return fmt.Errorf(format, arguments...)
}

// Indirectf is a printf wrapper through another wrapper of this package.
func Indirectf(format string, arguments ...any) {
	Logf(format, arguments...)
}

// Generic is a printf wrapper with a type parameter.
func Generic[T any](value T, format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

// Box has wrapper methods on a generic type.
type Box[T any] struct{ Value T }

// Printf is a printf wrapper on a generic receiver.
func (Box[T]) Printf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

// Print is a print wrapper on a pointer to a generic receiver.
func (*Box[T]) Print(arguments ...any) {
	fmt.Print(arguments...)
}

// Inner is embedded in parity's Outer, which promotes its wrapper.
type Inner struct{}

// Logf is a printf wrapper promoted through embedding.
func (Inner) Logf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

// Assigned is a function variable assigned a wrapper here, so it has a fact.
var Assigned func(format string, arguments ...any)

// Unassigned is a function variable parity assigns a wrapper to, so it has no fact and is a wrapper only there.
var Unassigned func(format string, arguments ...any)

// Hooks holds function fields, one assigned a wrapper here.
var Hooks struct {
	Logf  func(format string, arguments ...any)
	Other func(format string, arguments ...any)
}

func init() {
	Assigned = func(format string, arguments ...any) {
		fmt.Printf(format, arguments...)
	}
	Hooks.Logf = func(format string, arguments ...any) {
		fmt.Printf(format, arguments...)
	}
}

// NotWrapper changes its format before it forwards it, so it is no wrapper.
func NotWrapper(format string, arguments ...any) {
	format = "[" + format + "]"
	fmt.Printf(format, arguments...)
}
