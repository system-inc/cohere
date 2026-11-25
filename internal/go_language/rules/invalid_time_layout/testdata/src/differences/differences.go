// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the layout
// among several values, and time.Parse held in a variable, whose callee is known only once the variable is lifted, or
// not at all when two functions meet. The port and the original find the same here, so the fixture records no
// difference.
package differences

import "time"

func layoutAndValue() (string, string) {
	return "2006-13-01", ""
}

func statements() {
	defer time.Parse("2006-13-01", "")
	go time.Parse("2006-01-32", "")
	defer func() {
		time.Parse("15:61", "")
	}()
}

func multipleValues() {
	time.Parse(layoutAndValue())
}

func lifted(condition bool) {
	parse := time.Parse
	if condition {
		parse = time.Parse
	}
	parse("2006-13-01", "")
	choose := time.Parse
	if condition {
		choose = func(layout string, value string) (time.Time, error) { return time.Time{}, nil }
	}
	choose("2006-13-01", "")
}
