// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the string
// and its cutset, and a trimming function held in a variable, whose callee is known only once the variable is lifted,
// or not at all when two functions meet. The port and the original find the same here, so the fixture records no
// difference.
package differences

import "strings"

func valueAndCutset() (string, string) {
	return "value", "aa"
}

func statements(value string) {
	defer strings.Trim(value, "aa")
	go strings.TrimLeft(value, "bb")
	defer func() {
		_ = strings.TrimRight(value, "cc")
	}()
}

func multipleValues() {
	_ = strings.Trim(valueAndCutset())
	_ = strings.TrimRight(valueAndCutset())
}

func lifted(condition bool, value string) {
	trim := strings.Trim
	if condition {
		trim = strings.Trim
	}
	_ = trim(value, "aa")
	choose := strings.TrimLeft
	if condition {
		choose = strings.TrimRight
	}
	_ = choose(value, "aa")
}
