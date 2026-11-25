//go:build cgo

package reads

import "C"

// cgoCount is the most reads a call makes through cgo.
func cgoCount() int {
	return maxReads * 2
}
