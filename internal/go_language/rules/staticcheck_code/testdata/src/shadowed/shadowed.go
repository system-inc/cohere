// Package shadowed declares its own len, cap, nil and string at package scope, so no use of those names in it is the
// universe's, and staticcheck's Builtin pattern refuses every one, as the port must.
package shadowed

func len(values []int) int { return -1 }

func cap(values []int) int { return -1 }

var nil *int

type string []byte

func uses(values []int) {
	_ = len(values)
	_ = (cap)(values)
	_ = nil
	_ = (nil)
	_ = string("text")
	_ = append(values, 1)
	_ = true
}
