package core

import "github.com/system-inc/cohere/internal/rule"

// init registers yoda.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors
// landing at once touch nothing in common.
//
// The decoder is hand rolled because upstream's option is a two element TUPLE, an enum then an
// object, and this config layer keeps only the tuple's second element. Both halves therefore have
// to arrive through one argument, which the generic helper cannot express.
func init() {
	rule.Register(rule.Registration{
		Rule:   Yoda,
		Decode: DecodeYodaOptions,
	})
}
