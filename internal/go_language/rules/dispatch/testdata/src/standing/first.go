// Package standing holds functions an extra build leaves to the host and functions it keeps for itself, across two
// files, for the dispatchers to walk.
package standing

// kept is the build's own wherever anything stands: a function literal in a variable's initializer never does.
var kept = func() bool {
	if true {
		return true
	}
	return false
}

// stood stands in the tests, doc comment and all, with a function literal inside its body.
func stood(value int) int {
	if value > 0 {
		return 1
	}
	inner := func() int {
		if value < 0 {
			return -1
		}
		return 0
	}
	return inner()
}

func reached(value int) int {
	if value > 1 {
		return 1
	}
	return 0
}

func stoodToo() bool {
	if kept() {
		return true
	}
	return false
}
