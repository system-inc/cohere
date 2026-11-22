// Package stripped reads a package-level scanner in one body and checks its error in another, which the strip takes
// away.
package stripped

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
