package core

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers prefer-destructuring.
//
// Its own file rather than a line in the package's register.go: that shared list is one file every
// concurrent port has to edit, so two ports landing at once conflict on a file neither is changing.
//
// The Decode entry is load bearing for this rule beyond the usual reason. Upstream's option surface
// is two schema elements and this config layer delivers one, so the decoder is where
// `enforceForRenamedProperties` is rescued from silent loss and where the upstream array spelling is
// refused loudly instead of being half-honoured.
func init() {
	rule.Register(rule.Registration{
		Rule:   PreferDestructuring,
		Decode: DecodePreferDestructuringOptions,
	})
}
