// Package shadowed declares nil at package scope, so its condition compares with a variable, never the predeclared
// nil, and neither side reports it. Only a value comparable with a variable can be compared with it, which a slice or
// a map is not, so the range here is over a pointer to an array.
package shadowed

var nil *[3]int

func pointers(pointer *[3]int) {
	if pointer != nil {
		for range pointer {
		}
	}
}
