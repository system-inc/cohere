package flagged

import "strings"

// A function reads a var declared below it.
func retries() int {
	return retryLimit // want "`retryLimit` is a package-level variable declared further down this file"
}

// A function assigns a var declared below it.
func reset() {
	retryLimit = 0 // want "`retryLimit` is a package-level variable"
}

// A const is used above its declaration.
func quoted(text string) string {
	return strings.Repeat(prefix, 2) + text // want "`prefix` is a package-level constant"
}

// A method, and a function literal inside it, read the var.
type counter struct{ count int }

func (item *counter) full() bool {
	check := func() bool { return item.count >= retryLimit } // want "`retryLimit` is a package-level variable"
	return check()
}

// A generic function reads the const.
func clamp[Number int | int64](value Number) Number {
	if value > Number(ceiling) { // want "`ceiling` is a package-level constant"
		return Number(ceiling) // want "`ceiling` is a package-level constant"
	}
	return value
}

// An earlier var's initializer reads a later var, an array length a later const, and a map key another.
var doubled = retryLimit * 2 // want "`retryLimit` is a package-level variable"

var buffer [ceiling]byte // want "`ceiling` is a package-level constant"

var labels = map[string]int{prefix: 1} // want "`prefix` is a package-level constant"

// An earlier spec of a group reads a later one.
const (
	first  = second + 1 // want "`second` is a package-level constant"
	second = 1
)

var retryLimit = 3

const prefix = "> "

const ceiling = 16
