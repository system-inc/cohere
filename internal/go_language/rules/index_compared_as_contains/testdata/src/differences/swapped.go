package differences

import (
	strings "bytes"
)

// bytes imported under the name strings is spelled as the original wants and is the package the port wants, so the
// two agree, and the fix's strings.Contains is bytes.Contains, as the call names it.
func swapped(x []byte, y []byte) {
	_ = strings.Index(x, y) != -1
	_ = strings.IndexRune(x, 'r') < 0
}
