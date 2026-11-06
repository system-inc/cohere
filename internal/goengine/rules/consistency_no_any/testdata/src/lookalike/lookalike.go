// Package lookalike holds any and interface{} where they are no declaration's type, and names that look like them.
package lookalike

import (
	"fmt"

	"elsewhere"
)

// First takes any type at all, which is what a constraint says; the values themselves have a stated type.
func First[Value any](values []Value) Value {
	return values[0]
}

// Pair constrains both of its type parameters, one spelling the empty interface the old way.
type Pair[Key comparable, Value interface{}] struct {
	key   Key
	value Value
}

// Kind tells a generic value's type, which a type switch reads only through a conversion to an interface.
func Kind[Value any](value Value) string {
	switch any(value).(type) {
	case string:
		return "string"
	case []any, map[string]any:
		return "collection"
	}
	return "other"
}

// expressions writes the empty interface only inside expressions, where the value's type is handed to a place or
// read back rather than declared.
func expressions(value fmt.Stringer) {
	values := []any{1, "two"}
	index := map[string]interface{}{"one": 1}
	buffer := make([]any, 0, len(values))
	pointer := new(any)
	_, isAnything := any(value).(interface{})
	tests := []struct {
		input any
	}{{input: 1}}
	fmt.Println(values, index, buffer, pointer, isAnything, tests)
}

// named takes a parameter called any, which is a name rather than the type.
func named(any int) int {
	return any * 2
}

// shadowed declares its own type named any, which is not the predeclared one.
func shadowed() string {
	type any = string
	var name any = "local"
	return name
}

// stated names its types: a non-empty interface, an empty struct, and another package's name for any.
type stated struct {
	text   fmt.Stringer
	signal chan struct{}
	stored elsewhere.Value
}

// Words in a comment, any or interface{}, are not types, and neither is a string holding them.
var description = "any value, interface{} or not"
