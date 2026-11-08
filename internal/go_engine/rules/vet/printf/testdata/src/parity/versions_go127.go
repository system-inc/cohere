//go:build go1.27

package parity

import "fmt"

// A file of go1.27: %d takes no pointer, and %w no pointer to an error.
func versions127(number int, pointer *int, value parityError, values []int) {
	fmt.Printf("%d", pointer)    // Wrong type, use %p for a pointer.
	fmt.Printf("%d", &values)    // Clean: a top-level pointer to a slice of ints.
	fmt.Printf("%x", pointer)    // Clean.
	fmt.Printf("%d", number)     // Clean.
	_ = fmt.Errorf("%w", &value) // A pointer to an error type, which defeats errors.Is.
	_ = fmt.Errorf("%w", value)  // Clean.
}
