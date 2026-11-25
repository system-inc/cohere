// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the address
// among several values, and a function held in a variable, whose callee is known only once the variable is lifted,
// or not at all when two functions meet. The port and the original find the same here, so the fixture records no
// difference.
package differences

import "net/http"

func addressAndHandler() (string, http.Handler) {
	return "localhost", nil
}

func statements() {
	defer http.ListenAndServe("localhost", nil)
	go http.ListenAndServeTLS("localhost", "", "", nil)
	defer func() {
		http.ListenAndServe(":99999", nil)
	}()
}

func multipleValues() {
	http.ListenAndServe(addressAndHandler())
}

func lifted(condition bool) {
	listen := http.ListenAndServe
	if condition {
		listen = http.ListenAndServe
	}
	listen("localhost", nil)
	choose := http.ListenAndServe
	if condition {
		choose = func(address string, handler http.Handler) error { return nil }
	}
	choose("localhost", nil)
}
