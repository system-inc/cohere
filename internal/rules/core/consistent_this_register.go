package core

import "github.com/system-inc/verify/internal/rule"

// init registers consistent-this.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors landing
// at once touch nothing in common.
//
// The decoder is hand-rolled because upstream's option is a bare array of strings rather than an
// object, and because its default is `["that"]` rather than the zero value. A generic decoder would
// turn an absent option into an empty alias list, which does not disable the rule: it makes one half
// unreachable and makes the other report every capture of `this` under any name at all.
func init() {
	rule.Register(rule.Registration{
		Rule:   ConsistentThis,
		Decode: DecodeConsistentThisOptions,
	})
}
