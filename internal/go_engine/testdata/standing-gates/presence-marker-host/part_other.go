//go:build !windows

package gate

// use selects shape's method, in every build but windows's.
func use(value shape) { value.isShape() }
