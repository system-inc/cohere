// Package reader gives api.Logf, its own api.Logger's Logf field and api.Sink's Logf field promoted through a type of
// its own, in a file every build compiles, a literal each that hands its format and values to impl.Emit, which each
// platform's file of impl declares, forwarding to fmt.Printf in one build alone; and a platform file of its own alone
// gives api.Tracef a literal handing them to impl.Printf, a wrapper in every build. So each variable and field is a
// printf wrapper in reader in exactly one build, with no fact of it in either, and a call of any is a printf finding
// there alone.
package reader

import (
	"example.com/printfimportedhost/api"
	"example.com/printfimportedhost/impl"
)

// logger is an api.Logger whose Logf field reader gives a literal.
var logger api.Logger

// Wrapped embeds api.Sink, so Sink's Logf field is a Wrapped's, promoted.
type Wrapped struct {
	api.Sink
}

// wrapped is a Wrapped whose promoted Logf field reader gives a literal.
var wrapped Wrapped

// init gives api.Logf, logger's Logf field and wrapped's promoted one a literal each that hands its format and values
// to impl.Emit, so each wraps fmt.Printf in reader in exactly the build whose Emit does, each field on every value
// that holds it.
func init() {
	api.Logf = func(format string, args ...any) { impl.Emit(format, args...) }
	logger.Logf = func(format string, args ...any) { impl.Emit(format, args...) }
	wrapped.Logf = func(format string, args ...any) { impl.Emit(format, args...) }
}

// Target names only api.Logf, whose text and file are the same in every build.
func Target() { api.Logf("%d", "x") }

// TraceTarget names only api.Tracef, whose text and file are the same in every build.
func TraceTarget() { api.Tracef("%d", "x") }

// FieldTarget names only api.Logger and its Logf field, whose text and file are the same in every build.
func FieldTarget(each api.Logger) { each.Logf("%d", "x") }

// PromotedTarget names only Wrapped and api.Sink's Logf field through it, whose text and file are the same in every
// build.
func PromotedTarget(each Wrapped) { each.Logf("%d", "x") }
