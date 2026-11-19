// Package s declares Steady in a file every build compiles, and on windows a file that does not parse, under a line
// directive naming steady.go.
package s

// Steady is four in every build, and reads nothing that differs.
func Steady() int { return 4 }
