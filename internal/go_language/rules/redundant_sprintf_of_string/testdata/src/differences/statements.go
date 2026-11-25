package differences

import "fmt"

func statements(text string, other Text, raw []byte) {
	// The argument alone as a statement is a value evaluated and not used.
	fmt.Sprintf("%s", text) // port withholds: Remove unnecessary call to fmt.Sprintf
	// Its parentheses keep it a value evaluated and not used.
	(fmt.Sprintf("%s", text)) // port withholds: Remove unnecessary call to fmt.Sprintf
	// A conversion as a statement is a value evaluated and not used.
	fmt.Sprintf("%s", other) // port withholds: Replace with conversion to string
	// A conversion of a slice of bytes as a statement is too.
	fmt.Sprintf("%s", raw) // port withholds: Replace with conversion to string
	// A labeled statement is a statement too.
labeled:
	// The label's statement discards the value as any statement does.
	fmt.Sprintf("%s", other) // port withholds: Replace with conversion to string
	goto labeled
}

func deferred(other Text, raw []byte) {
	// Defer takes a call, never a conversion.
	defer fmt.Sprintf("%s", other) // port withholds: Replace with conversion to string
	// Go takes a call, never a conversion.
	go fmt.Sprintf("%s", raw) // port withholds: Replace with conversion to string
}
