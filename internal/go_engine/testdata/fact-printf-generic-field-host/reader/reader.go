// Package reader calls a Logger's Logf field, GenericLogger[any]'s, a printf finding in exactly the builds where the
// field is a wrapper.
package reader

import "example.com/printfgenericfieldhost/api"

// Target names only api.Logger, api.GenericLogger and its Logf field, whose text and file are the same in every
// build.
func Target(logger api.Logger) { logger.Logf("%d", "x") }
