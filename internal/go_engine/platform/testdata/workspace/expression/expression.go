// Package expression reads a dependency.Handle through a value no identifier names its type.
package expression

import "example.com/dependency"

// Next is one past dependency.Value.
func Next() int {
	handle := dependency.Value()
	return int(handle + 1)
}
