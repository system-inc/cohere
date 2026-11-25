// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the cutset
// among several values, and a function held in a variable, whose callee is known only once the variable is lifted,
// or not at all when two functions meet. The port and the original find the same here, so the fixture records no
// difference.
package differences

import "strings"

func textAndCutset() (string, string) {
	return "", "\xff"
}

func statements(text string) {
	defer strings.Trim(text, "\xff")
	go strings.IndexAny(text, "\xff")
	defer func() {
		strings.LastIndexAny(text, "\xff")
	}()
}

func multipleValues() {
	strings.Trim(textAndCutset())
}

func lifted(text string, condition bool) {
	trim := strings.Trim
	if condition {
		trim = strings.Trim
	}
	trim(text, "\xff")
	choose := strings.TrimLeft
	if condition {
		choose = strings.TrimRight
	}
	choose(text, "\xff")
}
