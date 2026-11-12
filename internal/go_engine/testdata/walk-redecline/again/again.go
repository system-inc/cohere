// Package again compares Again's error with nil.
package again

import "example.com/walkredecline/user"

// Failed is whether Again failed.
func Failed() bool { return user.Again() != nil }
