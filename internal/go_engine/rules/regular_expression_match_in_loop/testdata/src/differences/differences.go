// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the pattern
// and the text, and a matching function held in a variable, whose callee is known only once the variable is lifted,
// or not at all when two functions meet. The port and the original find the same here, so the fixture records no
// difference.
package differences

import "regexp"

func patternAndText() (string, string) {
	return `a`, "a"
}

func statements(values []string) {
	for _, value := range values {
		defer regexp.MatchString(`a`, value)
		go regexp.MatchString(`a`, value)
		defer func() {
			regexp.MatchString(`a`, value)
		}()
	}
}

func multipleValues(values []string) {
	for range values {
		regexp.MatchString(patternAndText())
	}
}

func lifted(condition bool, values []string) {
	for _, value := range values {
		match := regexp.MatchString
		if condition {
			match = regexp.MatchString
		}
		match(`a`, value)
		choose := regexp.MatchString
		if condition {
			choose = func(string, string) (bool, error) { return false, nil }
		}
		choose(`a`, value)
	}
}
