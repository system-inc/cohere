// Package reader calls api.Logger's Logf, a printf finding in exactly the builds where Logf is a wrapper.
package reader

import "example.com/printfinterfacehost/api"

// Target names only api.Logger and its Logf, whose text and file are the same in every build.
func Target(logger api.Logger) { logger.Logf("%d", "x") }
