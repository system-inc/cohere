//go:build !windows

package platform

import "fmt"

// Logf formats its arguments by format, as fmt.Sprintf does.
func Logf(format string, arguments ...any) string { return fmt.Sprintf(format, arguments...) }
