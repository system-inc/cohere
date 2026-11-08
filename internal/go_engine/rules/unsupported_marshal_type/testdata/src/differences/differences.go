// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the value
// among several, a method expression, which the representation calls through a thunk of its own, a function with no
// object that neither judges, and a marshal function held in a variable, whose callee is known only once the variable
// is lifted, or not at all when two functions meet. The port and the original find the same here, so the fixture
// records no difference.
package differences

import (
	"encoding/json"
	"encoding/xml"
	"io"
)

func valueAndIndents() (chan int, string, string) {
	return nil, "", "  "
}

func statements(channel chan int, writer io.Writer) {
	defer json.Marshal(channel)
	go xml.Marshal(channel)
	defer json.NewEncoder(writer).Encode(channel)
	defer func() {
		xml.MarshalIndent(channel, "", "  ")
	}()
}

func multipleValues() {
	json.MarshalIndent(valueAndIndents())
	xml.MarshalIndent(valueAndIndents())
}

func methodExpressions(channel chan int, writer io.Writer) {
	(*json.Encoder).Encode(json.NewEncoder(writer), channel)
	(*xml.Encoder).Encode(xml.NewEncoder(writer), channel)
}

func lifted(channel chan int, condition bool) {
	marshal := json.Marshal
	if condition {
		marshal = json.Marshal
	}
	marshal(channel)
	choose := xml.Marshal
	if condition {
		choose = func(value any) ([]byte, error) { return nil, nil }
	}
	choose(channel)
}
