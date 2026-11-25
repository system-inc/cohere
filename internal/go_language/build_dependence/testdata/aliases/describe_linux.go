//go:build linux

package aliases

// Describe is declared on linux only, on Alias, so Target's method set moves.
func (Alias) Describe() string { return "linux" }
