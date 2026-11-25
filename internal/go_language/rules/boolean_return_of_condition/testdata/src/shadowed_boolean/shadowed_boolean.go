// Package shadowed_boolean declares its own true at package scope, which is not the predeclared one, so neither side
// matches a return of it.
package shadowed_boolean

const true = false

func returnsTrue(value bool) bool {
	if value {
		return true
	}
	return false
}

func returnsTrueLast(value bool) bool {
	if value {
		return false
	}
	return true
}
