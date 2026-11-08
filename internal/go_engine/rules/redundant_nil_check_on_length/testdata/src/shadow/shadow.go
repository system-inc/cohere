// Package shadow declares a len of its own at package scope, which is not the universe's, so the query matches no
// call of it, though each check would match the query were it the builtin.
package shadow

func len(values []int) int {
	return 0
}

func checks(values []int) {
	if values == nil || len(values) == 0 {
	}
	if values != nil && len(values) > 0 {
	}
}
