package differences

import "fmt"

func shadowed(other Text, raw []byte) {
	string := 0
	_ = string
	// The block's string is an int, so the original's `string(other)` calls no conversion.
	_ = fmt.Sprintf("%s", other) // port withholds: Replace with conversion to string
	// Nor does its `string(raw)`.
	_ = fmt.Sprintf("%s", raw) // port withholds: Replace with conversion to string
}
