//go:build go1.26

package parity

import "fmt"

// A file of go1.26: an interface's methods are found as wrappers through the implementations assigned to it, %q
// takes only a byte or a rune of the integers, %d still takes a pointer and %w a pointer to an error.
type logger126 interface {
	Logf(format string, arguments ...any)
	Logln(arguments ...any)
	Other(format string, arguments ...any)
}

type embedding126 interface {
	logger126
}

type logger126Implementation struct{}

func (logger126Implementation) Logf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

func (*logger126Implementation) Logln(arguments ...any) {
	fmt.Println(arguments...)
}

func (logger126Implementation) Other(format string, arguments ...any) {}

var _ logger126 = &logger126Implementation{}

var _ embedding126 = logger126(nil)

type unassigned126 interface {
	Logf(format string, arguments ...any)
}

func versions126(log logger126, embedded embedding126, other unassigned126, number int, pointer *int,
	value parityError) {
	log.Logf("%d", "text")                // Wrong type string, through the interface.
	log.Logln("%d", number)               // Possible directive, through the interface.
	log.Other("%d", "text")               // Clean: its implementation is no wrapper.
	embedded.Logf("%d", "text")           // Wrong type string, through an embedded interface.
	other.Logf("%d", "text")              // Clean: nothing is assigned to it.
	fmt.Printf("%q", number)              // Wrong type int from go1.26.
	fmt.Printf("%q", byte(65))            // Clean.
	fmt.Printf("%q", rune(65))            // Clean.
	fmt.Printf("%q %q", "a", []byte("b")) // Clean.
	fmt.Printf("%d", pointer)             // Clean before go1.27.
	_ = fmt.Errorf("%w", &value)          // Clean before go1.27.
}
