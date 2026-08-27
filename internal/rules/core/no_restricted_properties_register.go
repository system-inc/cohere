package core

import "github.com/system-inc/verify/internal/rule"

// init registers no-restricted-properties.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors landing
// at once touch nothing in common.
//
// The decoder is hand-rolled to reject an entry naming neither an object nor a property, and to
// reject the two self-contradictory pairings upstream's schema forbids. Those are schema `anyOf` and
// `not` constraints upstream, which we have no equivalent for, so they are checked here or not at
// all -- and an entry naming neither would otherwise sit in the config matching nothing forever.
func init() {
	rule.Register(rule.Registration{
		Rule:   NoRestrictedProperties,
		Decode: DecodeNoRestrictedPropertiesOptions,
	})
}
