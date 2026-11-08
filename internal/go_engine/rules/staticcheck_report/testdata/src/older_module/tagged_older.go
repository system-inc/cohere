//go:build go1.18

package older_module

// taggedOlder is in a file tagged older than the module.
func taggedOlder(values []int) int {
	if len(values) > 1 {
		return values[1]
	}
	return 0
}
