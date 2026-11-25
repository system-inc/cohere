// Package early is checked before late, which imports it, and has findings of its own.
package early

import "fmt"

// Describe prints a count with a verb for text.
func Describe(count int) {
	fmt.Printf("%s items\n", count)
}
