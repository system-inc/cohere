// Package stripped imports fmt only for a body the strip takes away, and converts an integer to a string elsewhere.
package stripped

import "fmt"

// greet is the only user of fmt, and the strip takes its body.
func greet() { fmt.Println("hello") }

// digits converts an integer to a string of one rune, which the fix formats with the fmt already imported.
func digits(number int) string { return string(number) }
