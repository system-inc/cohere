// Package reader gives api.Logf and its own api.Logger's Logf field, in a file every build compiles, a literal each
// that hands its format and values to impl.Emit, which each platform's file of impl declares, forwarding to fmt.Printf
// in one build alone; and a platform file of its own alone gives api.Tracef a literal handing them to impl.Printf, a
// wrapper in every build. So each variable and the field is a printf wrapper in reader in exactly one build, with no
// fact of it in either, and a call of any is a printf finding there alone.
package reader

import (
	"example.com/printfimportedextra/api"
	"example.com/printfimportedextra/impl"
)

// logger is an api.Logger whose Logf field reader gives a literal.
var logger api.Logger

// init gives api.Logf and logger's Logf field a literal each that hands its format and values to impl.Emit, so each
// wraps fmt.Printf in reader in exactly the build whose Emit does, the field on every api.Logger.
func init() {
	api.Logf = func(format string, args ...any) { impl.Emit(format, args...) }
	logger.Logf = func(format string, args ...any) { impl.Emit(format, args...) }
}

// Target names only api.Logf, whose text and file are the same in every build.
func Target() { api.Logf("%d", "x") }

// TraceTarget names only api.Tracef, whose text and file are the same in every build.
func TraceTarget() { api.Tracef("%d", "x") }

// FieldTarget names only api.Logger and its Logf field, whose text and file are the same in every build.
func FieldTarget(each api.Logger) { each.Logf("%d", "x") }
