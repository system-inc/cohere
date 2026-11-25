//go:build !windows

package alone

// platformValue is pure here.
func platformValue(value int) int {
	return value + 2
}

// platformName is every other platform's.
func platformName() string {
	return "other"
}
