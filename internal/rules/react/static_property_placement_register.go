package react

import "github.com/system-inc/verify/internal/rule"

// init registers this package's `static-property-placement` rule.
//
// The decoder is hand-written rather than `rule.DecodeOptionsInto` because upstream's option surface
// is a POSITIONAL pair whose two slots have different types; see DecodeStaticPropertyPlacementOptions.
func init() {
	rule.Register(rule.Registration{
		Rule:   StaticPropertyPlacement,
		Decode: DecodeStaticPropertyPlacementOptions,
	})
}
