package q

import (
	"fmt"

	"example.com/directives/p"
)

// Prints formats p.Helper as an int, which on windows it is not: dependent.
func Prints() {
	fmt.Printf("%d", p.Helper())
}

// PrintsShared formats p.Shared, which no build changes: independent.
func PrintsShared() {
	fmt.Printf("%d", p.Shared())
}
