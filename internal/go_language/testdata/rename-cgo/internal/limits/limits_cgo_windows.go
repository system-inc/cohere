//go:build cgo

package limits

import "C"

// maxBytes is the most a read takes through cgo.
const maxBytes = 32

// Size is the most a read takes through cgo.
func Size() int {
	return int(C.int(32))
}
