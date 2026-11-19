// Package r declares Steady in a file every build compiles, and Extra on windows alone, in a file that names
// steady.go by a line directive.
package r

// Steady is four in every build, and reads nothing that differs.
func Steady() int { return 4 }
