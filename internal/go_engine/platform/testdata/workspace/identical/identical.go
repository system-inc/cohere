// Package identical references only objects read alike in both builds.
package identical

import "example.com/dependency"

// Read is dependency.Stable beside dependency.Name.
func Read() (int, string) { return dependency.Stable, dependency.Name }
