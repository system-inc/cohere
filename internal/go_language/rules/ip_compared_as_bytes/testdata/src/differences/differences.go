// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns both
// addresses, and bytes.Equal held in a variable, whose callee is known only once the variable is lifted, or not at all
// when two functions meet. The port and the original find the same here, so the fixture records no difference.
package differences

import (
	"bytes"
	"net"
)

func pair() (net.IP, net.IP) {
	return nil, nil
}

func addressAndBytes() (net.IP, []byte) {
	return nil, nil
}

func statements(first net.IP, second net.IP) {
	defer bytes.Equal(first, second)
	go bytes.Equal(first, second)
	defer func() {
		bytes.Equal(first, second)
	}()
}

func multipleValues() {
	bytes.Equal(pair())
	bytes.Equal(addressAndBytes())
}

func lifted(condition bool, first net.IP, second net.IP) {
	equal := bytes.Equal
	if condition {
		equal = bytes.Equal
	}
	equal(first, second)
	choose := bytes.Equal
	if condition {
		choose = bytes.EqualFold
	}
	choose(first, second)
}
