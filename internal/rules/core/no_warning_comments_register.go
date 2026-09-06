package core

import "github.com/system-inc/cohere/internal/rule"

// init registers no-warning-comments.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors landing
// at once touch nothing in common.
//
// The decoder is hand-rolled for two constraints the generic helper cannot express. An unrecognized
// location must error rather than silently select a third behaviour of matching nothing, since every
// arm is chosen by string equality. And a decoration entry must be exactly one non-whitespace
// character, which upstream states as a schema `pattern` and we have no schema layer to enforce.
func init() {
	rule.Register(rule.Registration{
		Rule:   NoWarningComments,
		Decode: DecodeNoWarningCommentsOptions,
	})
}
