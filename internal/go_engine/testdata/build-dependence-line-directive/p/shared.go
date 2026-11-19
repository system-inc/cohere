// Package p declares Helper an int on every build but windows, in a file that names shared.go by a line directive,
// and a string on windows.
package p

// Shared is two in every build, and reads nothing that differs.
func Shared() int { return 2 }
