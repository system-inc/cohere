//go:build linux

package localaliases

// local names Target in linux's file alone, so a method declared on it is Target's.
type local = Target

// Describe is declared on linux only, on local, so Target's method set moves.
func (local) Describe() string { return "linux" }
