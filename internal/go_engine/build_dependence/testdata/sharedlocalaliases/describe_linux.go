//go:build linux

package sharedlocalaliases

// Describe is declared on linux only, on local, so Target's method set moves.
func (local) Describe() string { return "linux" }
