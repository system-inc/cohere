//go:build linux

package methods

// Describe is declared on linux only.
func (Plain) Describe() string { return "linux" }
