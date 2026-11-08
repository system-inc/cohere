//go:build go1.23

package newer_module

// taggedNewer is in a file tagged newer than the module.
func taggedNewer(values []int) int {
	if len(values) > 2 {
		return values[2]
	}
	return 0
}
