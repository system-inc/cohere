// Package limits is a dependency whose objects read differently by the platform a build targets, and alike where
// it declares them once.
package limits

// Same is declared once, so every build reads it alike.
const Same = 1

// Shared is declared once, so every build reads it alike.
func Shared() int { return Same }

// Origin and Other share one struct type, Other's inferred from Origin, whose field Left is read from the first of
// them by name.
var (
	Origin = struct{ Left int }{}
	Other  = Origin
)
