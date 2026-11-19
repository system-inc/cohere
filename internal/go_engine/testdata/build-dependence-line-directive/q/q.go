// Package q formats p's Helper as an int, which on windows it is not.
package q

import (
	"fmt"

	"example.com/dependenceline/p"
)

// Prints is Helper formatted with %d.
func Prints() string {
	return fmt.Sprintf("%d", p.Helper())
}

// PrintsShared is Shared formatted with %d, which no build changes.
func PrintsShared() string {
	return fmt.Sprintf("%d", p.Shared())
}

// Alone reads nothing of p.
func Alone() int { return 3 }
