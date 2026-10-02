package rule

import "fmt"

// OptionsAs reads the options a rule's Run is handed as the type its registration's decoder
// returns, in the same two-value shape as the comma-ok assertion it replaces.
//
//	settings, ok := rule.OptionsAs[NoConcatenatedClassesOptions](options)
//	if !ok {
//		settings = defaultNoConcatenatedClassesSettings()
//	}
//
// Nil is the one honest miss: a rule configured as a bare `"error"` is handed no options, so it
// gets the zero value and false, and the rule's own default arm runs exactly as it did before.
//
// Any other mismatch panics, because it can only mean the decoder and the assertion disagree. Run
// takes `options any`, so the compiler never compares the type a Decode returns with the type a rule
// asserts, and a comma-ok assertion cannot fail loudly: `unified-signatures` asserted a pointer over
// the value `DecodeOptionsInto` returns, the assertion missed on every configured run, and both of
// its options read false whatever the config said, with every fixture green because the fixtures
// handed Run the pointer directly (#qmvkf83). A panic turns that into a crashed file in the run and a
// failing test for any fixture that configures the rule.
//
// The message names both package-qualified types, which together name the rule.
func OptionsAs[Options any](options any) (Options, bool) {
	var zero Options
	if options == nil {
		return zero, false
	}
	typed, matches := options.(Options)
	if !matches {
		panic(fmt.Sprintf("the rule reads its options as %T, but its decoder handed it %T: the "+
			"registration's Decode and the rule's OptionsAs must name the same type, or every "+
			"configured option is ignored", zero, options))
	}
	return typed, true
}
