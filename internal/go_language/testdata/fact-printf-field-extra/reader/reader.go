// Package reader calls a Logger's Logf field, a printf finding in exactly the builds where the field is a wrapper.
package reader

import "example.com/printffieldextra/api"

// Target names only api.Logger and its Logf field, whose text and file are the same in every build.
func Target(logger api.Logger) { logger.Logf("%d", "x") }
