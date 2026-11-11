// Package inferrer instantiates generic.Identity with platform.Handle, which it never names: the type argument is
// inferred from Get's result.
package inferrer

import (
	"example.com/restoreinstance/generic"
	"example.com/restoreinstance/platform"
)

// Same passes a Handle through Identity, which copies it.
func Same() {
	handle := platform.Get()
	_ = generic.Identity(handle)
}
