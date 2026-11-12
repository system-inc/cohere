//go:build cohere_extra && !windows

package platform

import "fmt"

// Logf is its format, its arguments left alone.
func Logf(format string, arguments ...any) string { return fmt.Sprint(format) + string(rune(len(arguments))) }
