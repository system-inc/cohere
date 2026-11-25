//go:build go1.23

package parity

import (
	"fmt"
	"log"
)

// A file of go1.23: the non-constant format check is off.
func nonConstant123(format string) {
	fmt.Printf(format)
	log.Printf(format)
	logf(format)
}
