//go:build !windows

package api

import "fmt"

// emit prints args by format, so it and Logf wrap fmt.Printf in this build.
func emit(format string, args ...any) {
	fmt.Printf(format, args...)
}
