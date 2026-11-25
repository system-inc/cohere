//go:build !windows

package alone

// platformName is every other platform's.
func platformName() string {
	return "other"
}
