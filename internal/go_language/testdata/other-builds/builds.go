// Package builds joins two parts of a path with the separator each platform uses, after the label each build sets.
// A file of separator only windows builds, and one of label only a build with the cohere_extra tag builds, so a run
// that checks every build the files name checks three.
package builds

import (
	"example.com/builds/internal/label"
	"example.com/builds/internal/separator"
)

// Joined is first and second joined by the platform's separator, after the build's label.
func Joined(first string, second string) string {
	return label.Prefix() + first + separator.Separator + second
}
