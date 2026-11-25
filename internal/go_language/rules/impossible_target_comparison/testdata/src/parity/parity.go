// Package parity holds the shapes SA4032 depends on, in files constrained to each target the test loads: how a
// comparison of runtime.GOOS or runtime.GOARCH is written, which the patterns accept and which they refuse, and the
// constraints that rule a value out, leave it possible, or are too wide to judge. Each comparison's comment says what
// the original makes of it; the parity test holds only that the port makes the same.
package parity

import "runtime"

func use(values ...any) {}

// Not found: a file with no constraint is never judged.
func unconstrained() {
	use(runtime.GOOS == "windows")
}
