//go:build go1.21

package newer_module

// taggedOlder is in a file tagged older than the module.
func taggedOlder(values []int) int {
	if len(values) > 1 {
		return values[1]
	}
	return 0
}
