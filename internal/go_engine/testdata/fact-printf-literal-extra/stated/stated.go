// Package stated calls api's variables whose type is stated, a printf finding in exactly the builds where the variable
// is a wrapper. It is a package apart from reader, whose Target the reach reaches, so no body here shares a package
// with a body the build judges dependent.
package stated

import "example.com/printfliteralextra/api"

// TypedTarget names only api.TypedLogf, whose text and file are the same in every build.
func TypedTarget() { api.TypedLogf("%d", "x") }

// AssignedTarget names only api.AssignedLogf, whose text and file are the same in every build.
func AssignedTarget() { api.AssignedLogf("%d", "x") }
