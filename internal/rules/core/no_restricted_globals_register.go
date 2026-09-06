package core

import "github.com/system-inc/cohere/internal/rule"

// init registers no-restricted-globals.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors
// landing at once touch nothing in common.
//
// The decoder is hand rolled because the option is a union of two wire shapes, a bare array and an
// object carrying that array plus two flags, and each list entry is itself either a string or an
// object. The generic helper expresses none of those alternatives.
func init() {
	rule.Register(rule.Registration{
		Rule:   NoRestrictedGlobals,
		Decode: DecodeNoRestrictedGlobalsOptions,
	})
}
