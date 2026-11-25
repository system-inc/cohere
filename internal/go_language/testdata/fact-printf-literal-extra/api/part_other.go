//go:build !windows

package api

import "fmt"

// emit formats the format and how many values it was handed, and drops the text, so it wraps nothing.
func emit(format string, args ...any) {
	_ = fmt.Sprint(format, len(args))
}
