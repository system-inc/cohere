// Package paths names what a platform puts between two parts of a path. Separator reads a dependency's constant,
// os.PathSeparator, which windows declares apart, and Mode a constant of its own, which windows's file declares apart
// too, so a run that checks every build checks windows beside the host's.
package paths

import "os"

// Separator is the platform's path separator.
func Separator() rune {
	return os.PathSeparator
}

// Joined is first and second joined by a slash, which no build changes.
func Joined(first string, second string) string {
	return first + "/" + second
}

// Mode is the platform's mode.
func Mode() string {
	return mode
}
