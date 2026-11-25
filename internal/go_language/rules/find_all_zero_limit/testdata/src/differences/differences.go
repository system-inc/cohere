// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the limit
// among several values, a method expression, whose receiver is its first argument, and a method value, whose callee
// is a wrapper that names no function of the rules. The port and the original find the same here, so the fixture
// records no difference.
package differences

import "regexp"

var pattern = regexp.MustCompile(`a`)

func inputAndLimit() ([]byte, int) {
	return nil, 0
}

func statements(input []byte) {
	defer pattern.FindAll(input, 0)
	go pattern.FindAllString("", 0)
	defer func() {
		pattern.FindAllIndex(input, 0)
	}()
}

func multipleValues() {
	pattern.FindAll(inputAndLimit())
}

func receivers(input []byte) {
	(*regexp.Regexp).FindAll(pattern, input, 0)
	bound := pattern.FindAllSubmatch
	bound(input, 0)
}
