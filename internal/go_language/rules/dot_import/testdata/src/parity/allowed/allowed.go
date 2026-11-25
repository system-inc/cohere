// Package allowed is under a staticcheck.conf that allows one path, so its dot import of it is left alone, and its dot
// import of any other path is reported.
package allowed

import (
	. "math"
	. "strings"
)

var (
	_ = ToUpper
	_ = Abs
)
