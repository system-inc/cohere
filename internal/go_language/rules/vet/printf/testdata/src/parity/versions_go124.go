//go:build go1.24

package parity

import (
	"fmt"
	"log"
	"os"
)

// A file of go1.24: a non-constant format with no other arguments is reported, with its fix.
func nonConstant124(format string, number int) {
	fmt.Printf(format)             // Non-constant format string.
	fmt.Printf(format, number)     // Clean, with arguments.
	fmt.Fprintf(os.Stderr, format) // Non-constant format string.
	log.Printf(format)             // Non-constant format string.
	logf(format)                   // Non-constant format string, through a wrapper.
	_ = fmt.Errorf(format)         // Non-constant format string, an errorf.
	fmt.Printf("%d", number)       // Clean.
	fmt.Printf(format + "\n")      // Non-constant format string, an expression.
	const constant = "%d"
	fmt.Printf(constant, number) // Clean, a named constant.
	fmt.Printf(constant)         // Reads arg #1 of 0, at the whole argument.
	fmt.Printf("%q", number)     // Clean before go1.26.
}
