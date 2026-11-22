//go:build !windows

package api

import "fmt"

// platform is a GenericLogger[any] whose Logf prints, so the generic field wraps fmt.Printf in this build.
var platform GenericLogger[any]

func init() {
	platform.Logf = func(format string, args ...any) { fmt.Printf(format, args...) }
}
