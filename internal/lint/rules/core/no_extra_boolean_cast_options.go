package core

import "github.com/system-inc/cohere/internal/lint/rule"

// NoExtraBooleanCastOptions configures whether inner expressions are checked.
type NoExtraBooleanCastOptions struct {
	// EnforceForInnerExpressions extends the rule through expressions whose *result* lands in a
	// boolean context rather than the cast itself: the operands of `&&` and `||`, the right side of
	// `??`, both branches of a ternary, and the last element of a sequence.
	//
	// Off by default in both upstreams, because the extension is not a pure superset in intent.
	// `foo || Boolean(bar)` coerces only when the whole expression is tested, and an author may be
	// building a value rather than a condition.
	EnforceForInnerExpressions bool `json:"enforceForInnerExpressions"`
}

// UnmarshalJSON accepts the deprecated `enforceForLogicalOperands` spelling as a synonym.
//
// This is not cosmetic and it is not quite parity either. ESLint declares the two names as
// *different options*: the older one recurses only through logical operators, while the newer one
// also recurses through `??`, ternary branches, and sequence tails. oxc collapses them with a serde
// alias, so under oxc the deprecated name silently buys the newer, wider behaviour.
//
// This port follows oxc, because oxc is the port target. The divergence is real and measurable:
// upstream's own corpus runs 104 cases under the old spelling and 118 under the new, and the 14
// extra are exactly the `??`, ternary-branch and sequence-tail shapes ESLint's legacy mode would
// decline. Under this rule all 118 report regardless of which name was written. Latent for us today
// because our config passes no options at all, and worth stating rather than discovering.
func (options *NoExtraBooleanCastOptions) UnmarshalJSON(raw []byte) error {
	var decoded struct {
		EnforceForInnerExpressions *bool `json:"enforceForInnerExpressions"`
		EnforceForLogicalOperands  *bool `json:"enforceForLogicalOperands"`
	}
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &decoded); unmarshalOptionsError != nil {
		return unmarshalOptionsError
	}
	// The newer name wins when both are written, matching serde's alias resolution, where the
	// canonical field name takes the value and the alias only fills an absent one.
	switch {
	case decoded.EnforceForInnerExpressions != nil:
		options.EnforceForInnerExpressions = *decoded.EnforceForInnerExpressions
	case decoded.EnforceForLogicalOperands != nil:
		options.EnforceForInnerExpressions = *decoded.EnforceForLogicalOperands
	}
	return nil
}
