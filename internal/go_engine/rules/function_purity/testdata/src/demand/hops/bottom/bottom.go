// Package bottom is two imports below package top: its exported function's fact stops one import up, at middle, and
// its method's goes all the way.
package bottom

// Count is a basic type with a method.
type Count int

// Pure is pure, and visible to middle, which imports bottom, but not to top.
func Pure(value int) int {
	return value*2 + 1
}

// Double is pure, and as a method visible to every package above.
func (count Count) Double() int {
	return int(count)*2 + 1
}
