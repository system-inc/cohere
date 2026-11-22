// Package r is the same in every build, and calls m's Finish before a value it assigns twice, so the first value is
// never read where Finish returns, the host's build, and the code after the call is unreachable on windows, where it
// stops: SA4006 reports the value on the host alone, and merges only where every build reports it.
package r

import "example.com/restorenoreturn/m"

// Twice is second, assigned over first once Finish has returned.
func Twice(first int, second int) int {
	m.Finish()
	value := first
	value = second
	return value
}
