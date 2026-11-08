//go:build go1.25

package parity

import "fmt"

// A file of go1.25: an interface's methods are not found as wrappers, and %q still takes any integer.
type logger125 interface {
	Logf(format string, arguments ...any)
}

type logger125Implementation struct{}

func (logger125Implementation) Logf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

var _ logger125 = logger125Implementation{}

func versions125(log logger125, number int, pointer *int) {
	log.Logf("%d", "text")    // Clean: not found through the interface before go1.26.
	fmt.Printf("%q", number)  // Clean before go1.26.
	fmt.Printf("%d", pointer) // Clean before go1.27.
}
