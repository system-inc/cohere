// Package errorchecked is scanner_error's stripped fixture in presence's form: a package-level scanner read in one
// body, read, and its error checked in another, check.
package errorchecked

import (
	"bufio"
	"os"
)

var input = bufio.NewScanner(os.Stdin)

// read loops on the scanner and never checks its error.
func read() {
	for input.Scan() {
	}
}

// check is where the scanner's error is checked, and the strip takes its body.
func check() error { return input.Err() }
