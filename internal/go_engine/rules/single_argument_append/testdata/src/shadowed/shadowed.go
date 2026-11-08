// Package shadowed declares append at package scope, so no call in it names the builtin. It is a package of its own
// since the declaration shadows append in every file of its package.
package shadowed

var append = func(values []int) []int { return values }

// Not found: append is the package's variable.
func call(values []int) []int {
	return append(values)
}

// Not found: in parentheses too.
func parenthesized(values []int) []int {
	return (append)(values)
}
