// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the value
// among several, and a function held in a variable, whose callee is known only once the variable is lifted, or not
// at all when two functions meet or the function is a method bound to its receiver. The port and the original find
// the same here, so the fixture records no difference.
package differences

import (
	"encoding/json"
	"encoding/xml"
)

type hidden struct{ value int }

func dataAndValue() ([]byte, *hidden) {
	return nil, &hidden{}
}

func statements(encoder *json.Encoder) {
	defer json.Marshal(hidden{})
	go xml.Marshal(hidden{})
	defer encoder.Encode(hidden{})
	defer func() {
		json.MarshalIndent(hidden{}, "", " ")
	}()
}

func multipleValues() {
	json.Unmarshal(dataAndValue())
	xml.Unmarshal(dataAndValue())
}

func lifted(encoder *json.Encoder, condition bool) {
	marshal := json.Marshal
	if condition {
		marshal = json.Marshal
	}
	marshal(hidden{})
	choose := json.Marshal
	if condition {
		choose = xml.Marshal
	}
	choose(hidden{})
	encode := encoder.Encode
	encode(hidden{})
	expression := (*json.Encoder).Encode
	expression(encoder, hidden{})
}
