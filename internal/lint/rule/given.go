package rule

import "sync/atomic"

// OptionsReads counts every Given.Read call, so the registry's decoder agreement test can tell a rule whose
// options read was reached from one that ran without reaching it (#zwd43jn). One atomic add per options read,
// contended across the parallel file walk. Measured on ahra's full lint, five runs each: a median lint phase of
// 3.88s with the counter and 4.07s without, against a run-to-run spread of 3.4s to 5.8s, so its cost is below
// what a run resolves rather than shown to be zero.
var OptionsReads atomic.Int64

// Given is the options a rule's Run and ProgramFingerprint are handed: the value its registration's decoder
// returned, or none when no decoder produced any.
//
//	settings, configured := options.Read()
//	if !configured {
//		settings = defaultNoConcatenatedClassesSettings()
//	}
//
// The type is the rule's own Options, fixed where the rule is declared, so there is nothing to assert and
// nothing to drift: before this, options travelled as `any`, and `unified-signatures` asserted a pointer over
// the value its decoder returned, so both of its options read false whatever the config said (#qmvkf83).
// That mismatch is now a compile error in the Registration that pairs the two.
type Given[Options any] struct {
	value      Options
	configured bool
}

// Configured is the options a rule's decoder produced, as it returned them. The zero Given is a rule no decoder
// produced options for.
func Configured[Options any](value Options) Given[Options] {
	return Given[Options]{value: value, configured: true}
}

// Read returns the decoded options and whether a decoder produced them, which is not the same as whether the
// config wrote any. A bare severity reads false only for a rule that takes no options or whose decoder refuses
// empty input, as DecodeOptionsInto does, since the config layer then falls back to the rule's Unconfigured. A
// decoder that fills defaults from nothing, which every DecodeAt decoder is handed for a bare severity, returns
// its defaults configured, and Read reads true. Without options it returns the zero value and false, so the
// rule's own default arm runs.
func (options Given[Options]) Read() (Options, bool) {
	OptionsReads.Add(1)
	return options.value, options.configured
}
