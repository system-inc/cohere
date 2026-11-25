//go:build go1.21

package older_module

// taggedNewer is in a file tagged newer than the module.
func taggedNewer(values []int) int {
	if len(values) > 2 {
		return values[2]
	}
	return 0
}
