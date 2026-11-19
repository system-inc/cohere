// Package s declares Steady in a file every build compiles. The test that copies it writes beside it
// broken_windows.go, which windows alone compiles, under a line directive naming steady.go, and which does not parse,
// so no file here fails a parse of the repository's own.
package s

// Steady is four in every build, and reads nothing that differs.
func Steady() int { return 4 }
