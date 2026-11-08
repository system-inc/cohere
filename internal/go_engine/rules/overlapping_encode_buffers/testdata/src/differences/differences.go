// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns both
// buffers, and an encoder held in a variable or as a method value, whose callee is known only once the variable is
// lifted, or not at all when two functions meet. The port and the original find the same here, so the fixture records
// no difference.
package differences

import (
	"encoding/base64"
	"encoding/hex"
)

var buffer = make([]byte, 8)

func sameBuffers() ([]byte, []byte) {
	return buffer, buffer
}

func statements(local []byte) {
	defer hex.Encode(local, local)
	go hex.Encode(local, local)
	defer func() {
		base64.StdEncoding.Encode(local, local)
	}()
}

func multipleValues() {
	hex.Encode(sameBuffers())
}

func lifted(condition bool, local []byte) {
	encode := hex.Encode
	if condition {
		encode = hex.Encode
	}
	encode(local, local)
	choose := hex.Encode
	if condition {
		choose = func(destination []byte, source []byte) int { return 0 }
	}
	choose(local, local)
	method := base64.StdEncoding.Encode
	method(local, local)
}
