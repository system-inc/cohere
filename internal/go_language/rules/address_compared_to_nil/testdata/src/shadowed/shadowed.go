// Package shadowed declares its own nil at package scope, so no comparison in it is with the builtin, and the pattern's
// Builtin refuses every one, as the port must.
package shadowed

var nil *int

func compared(value int) bool {
	_ = &value == nil
	_ = &value != (nil)
	return &value == nil
}
