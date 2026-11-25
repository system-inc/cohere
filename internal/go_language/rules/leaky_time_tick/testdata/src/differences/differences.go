// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation, and each question the port asks
// in place of the original's, of the standard library's version, the package and the file, is a port proven beside
// the one it replaces. They could disagree where those answers are least plain: a file tagged with a version in a
// package with none, a package initializer, a generic function, and a literal in a test file whose enclosing function
// is not.
package differences

import "time"

// Found by both: a user's init function has a position, and returns.
func init() {
	<-time.Tick(time.Second)
}

// Found by both: a generic function that returns, judged once, as written.
func generic[Value any](value Value) Value {
	<-time.Tick(time.Second)
	return value
}

// Found by both: a package variable's initializer calls a literal that returns.
var first = func() time.Time {
	return <-time.Tick(time.Second)
}()
