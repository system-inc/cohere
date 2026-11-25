// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the count
// among several values, and a function held in a variable, whose callee is known only once the variable is lifted,
// or not at all when two functions meet. The port and the original find the same here, so the fixture records no
// difference.
package differences

import (
	"bytes"
	"strings"
)

func arguments() (string, string, string, int) {
	return "", "", "", 0
}

func statements(text string) {
	defer strings.Replace(text, "a", "b", 0)
	go bytes.Replace(nil, nil, nil, 0)
	defer func() {
		strings.Replace(text, "a", "b", 0)
	}()
}

func multipleValues() {
	strings.Replace(arguments())
}

func lifted(text string, condition bool) {
	replace := strings.Replace
	if condition {
		replace = strings.Replace
	}
	replace(text, "a", "b", 0)
	choose := strings.Replace
	if condition {
		choose = func(text string, old string, new string, count int) string { return text }
	}
	choose(text, "a", "b", 0)
}
