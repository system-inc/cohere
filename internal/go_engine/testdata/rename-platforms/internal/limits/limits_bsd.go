//go:build freebsd

package limits

// extra is what freebsd adds to a read.
func extra() int {
	return maxBytes / 4
}
