// Package user reaches the dependency, so a build's load reads it, and gives its importers the same under every
// platform.
package user

import "example.com/dependency/limits"

// Use calls what every platform declares.
func Use() int { return limits.Shared() + limits.Size }
