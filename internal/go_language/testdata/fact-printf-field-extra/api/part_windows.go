package api

import "fmt"

// platform is a Logger whose Logf prints, so the field wraps fmt.Printf in this build.
var platform Logger

func init() {
	platform.Logf = func(format string, args ...any) { fmt.Printf(format, args...) }
}
