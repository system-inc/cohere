// Package skipped has the import path bytes_test, the path of bytes' external tests, which the check leaves alone.
package skipped

import "bytes"

func skipped(buffer *bytes.Buffer) ([]uint8, any) {
	return []byte(buffer.String()), string(buffer.Bytes())
}
