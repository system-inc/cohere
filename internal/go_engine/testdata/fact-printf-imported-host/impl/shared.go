// Package impl declares Printf, which forwards to fmt.Printf in every build, in a file every build compiles, and Emit,
// which each platform's file declares, forwarding to fmt.Printf in one build alone.
package impl

import "fmt"

// Printf prints args by format, so it wraps fmt.Printf in every build.
func Printf(format string, args ...any) {
	fmt.Printf(format, args...)
}
