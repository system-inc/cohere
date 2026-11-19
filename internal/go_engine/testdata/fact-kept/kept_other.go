//go:build !windows

package kept

// platformBase is every other platform's, and pure.
func platformBase(value int) int {
	return value + 1
}
