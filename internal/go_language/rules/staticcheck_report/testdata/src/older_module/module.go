// Package older_module is a module at go 1.20, with one file of no build line and one tagged on each side of it.
package older_module

// untagged has the module's own version.
func untagged(values []int) int {
	if len(values) > 0 {
		return values[0]
	}
	return 0
}
