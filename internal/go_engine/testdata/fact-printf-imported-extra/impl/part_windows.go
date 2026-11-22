package impl

import "fmt"

// Emit prints args by format, so it wraps fmt.Printf in this build.
func Emit(format string, args ...any) {
	fmt.Printf(format, args...)
}
