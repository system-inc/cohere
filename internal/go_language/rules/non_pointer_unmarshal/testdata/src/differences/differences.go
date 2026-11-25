// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the target
// among several values, a method whose receiver the kit leaves out of the arguments, a method expression, whose
// receiver is its first argument, and a method value, whose callee is a wrapper that names no function of the rules.
// The port and the original find the same here, so the fixture records no difference.
package differences

import (
	"encoding/json"
	"io"
)

type record struct{}

func dataAndTarget() ([]byte, record) {
	return nil, record{}
}

func statements(data []byte, reader io.Reader) {
	defer json.Unmarshal(data, record{})
	go json.NewDecoder(reader).Decode(record{})
	defer func() {
		json.Unmarshal(data, record{})
	}()
}

func multipleValues() {
	json.Unmarshal(dataAndTarget())
}

func receivers(reader io.Reader) {
	decoder := json.NewDecoder(reader)
	(*json.Decoder).Decode(decoder, record{})
	bound := decoder.Decode
	bound(record{})
}
