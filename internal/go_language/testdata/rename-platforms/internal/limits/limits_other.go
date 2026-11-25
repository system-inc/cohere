//go:build !windows && !freebsd

package limits

// extra is what every other platform adds to a read: nothing.
func extra() int {
	return 0
}
