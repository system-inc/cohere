// Package skipped has the import path bytes_test, the path of bytes' external tests, which the check leaves alone.
package skipped

import "bytes"

func skipped(first []byte, second []byte) bool {
	return bytes.Compare(first, second) == 0
}
