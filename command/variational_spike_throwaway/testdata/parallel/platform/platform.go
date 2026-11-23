// Package platform declares a handle, a separator and a count whose types differ by build, so every package of ours
// that reads them is checked with answers that fork.
package platform

import "fmt"

// Describe spells a handle with the build's separator and name.
func Describe(handle Handle) string {
	return fmt.Sprintf("%v%c%s", handle, Separator, Name)
}

// Doubled is the build's count twice over.
func Doubled() Count {
	return Lines + Lines
}
