// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, and url.Parse held in a variable,
// whose callee is known only once the variable is lifted, or not at all when two functions meet. url.Parse takes one
// argument, so f(g()) cannot reach it. The port and the original find the same here, so the fixture records no
// difference.
package differences

import "net/url"

func statements() {
	defer url.Parse(":")
	go url.Parse("%")
	defer func() {
		url.Parse("http://[::1")
	}()
}

func lifted(condition bool) {
	parse := url.Parse
	if condition {
		parse = url.Parse
	}
	parse(":")
	choose := url.Parse
	if condition {
		choose = url.ParseRequestURI
	}
	choose(":")
}
