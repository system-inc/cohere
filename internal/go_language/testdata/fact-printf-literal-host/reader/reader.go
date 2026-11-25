// Package reader calls api.Logf, a printf finding in exactly the builds where Logf is a wrapper.
package reader

import "example.com/printfliteralhost/api"

// Target names only api.Logf, whose text and file are the same in every build.
func Target() { api.Logf("%d", "x") }
