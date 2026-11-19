//go:build linux

package shadowed

// min is declared on linux only, at package level, so it shadows the universe's min in every file of the package.
func min(left int, right int) int { return left + right - max(left, right) }
