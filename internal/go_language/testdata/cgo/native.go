// Package native calls C.
package native

// #include <stdlib.h>
import "C"

// Absolute is the magnitude of value, from C's abs.
func Absolute(value int) int {
	return int(C.abs(C.int(value)))
}
