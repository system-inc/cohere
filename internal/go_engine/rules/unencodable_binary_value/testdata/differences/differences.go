// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the data
// among several values, and binary.Write held in a variable, whose callee is known only once the variable is lifted,
// or not at all when two functions meet. The port and the original find the same here, so the fixture records no
// difference.
package differences

import (
	"encoding/binary"
	"io"
	"io/ioutil"
)

func dataAndOrder() (io.Writer, binary.ByteOrder, uint) {
	return ioutil.Discard, binary.BigEndian, 0
}

func statements(writer io.Writer, order binary.ByteOrder) {
	defer binary.Write(writer, order, int(0))
	go binary.Write(writer, order, "text")
	defer func() {
		binary.Write(writer, order, []int{})
	}()
}

func multipleValues() {
	binary.Write(dataAndOrder())
}

func lifted(writer io.Writer, order binary.ByteOrder, condition bool) {
	write := binary.Write
	if condition {
		write = binary.Write
	}
	write(writer, order, int(0))
	choose := binary.Write
	if condition {
		choose = func(writer io.Writer, order binary.ByteOrder, data any) error { return nil }
	}
	choose(writer, order, int(0))
}
