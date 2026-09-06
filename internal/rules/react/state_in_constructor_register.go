package react

import "github.com/system-inc/cohere/internal/rule"

// init registers this package's `state-in-constructor` rule.
//
// A file of its own rather than a line in `register.go`, so a port is three owned files and no
// shared edit. The decoder is hand-written rather than `rule.DecodeOptionsInto` because an
// unrecognized mode has to fail loudly; see DecodeStateInConstructorOptions.
func init() {
	rule.Register(rule.Registration{Rule: StateInConstructor, Decode: DecodeStateInConstructorOptions})
}
