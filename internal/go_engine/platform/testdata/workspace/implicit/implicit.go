// Package implicit binds a dependency.Shape in a type switch whose clauses never name it.
package implicit

import "example.com/dependency"

// Bound is whether the binding of the switch's default clause is set.
func Bound() bool {
	switch bound := dependency.Make().(type) {
	case nil:
		return false
	default:
		return bound != nil
	}
}
