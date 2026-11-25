package store

import "fmt"

// Describe says what a value is, through a type switch whose symbol no declaration holds: go/types gives each case
// its own object and the symbol none.
func Describe(input fmt.Stringer) string {
	switch msg := input.(type) {
	case nil:
		return "nothing"
	default:
		return msg.String()
	}
}
