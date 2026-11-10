//go:build !windows

package platform

import "fmt"

// Size is the value's size.
func (shared Shared) Size() int { return shared.Value }

// Open is an error that is never a nil interface, holding a nil *Failure.
func Open() error {
	var failure *Failure
	return failure
}

// Logf formats its arguments by format, as fmt.Sprintf does.
func Logf(format string, arguments ...any) string { return fmt.Sprintf(format, arguments...) }
