package core

// EqeqeqOptions configures the rule.
//
// Upstream's option surface is a positional list whose legal second element depends on the first:
// `["always", {"null": ...}]` is valid and `["smart", {"null": ...}]` is not, which its schema
// expresses as a two-branch `anyOf`. `DecodeEqeqeqOptions` reads that list and enforces the same
// dependency, and this struct is what it decodes to.
//
// Upstream also accepts `"allow-null"` as a deprecated first element. Its own code reads it as
// `nullOption = config === "always" ? ... : "ignore"` with no smart exemption, which is Always plus
// a null policy of Ignore, so that is what it decodes to.
type EqeqeqOptions struct {
	// Mode is how strictly to ask. Absent means Always, which is upstream's default.
	Mode EqeqeqMode

	// Null is what to do about a comparison against the `null` literal.
	//
	// Absent means Always under the Always mode, matching upstream's `options.null || "always"`.
	// Under Smart it is forced to Ignore, because Smart already exempts every null comparison and
	// upstream hardcodes `nullOption = "ignore"` for it.
	Null EqeqeqNullPolicy
}

// resolve applies upstream's defaults, including the one that depends on the mode.
func (options EqeqeqOptions) resolve() eqeqeqSettings {
	settings := eqeqeqSettings{mode: options.Mode, null: options.Null}
	if settings.mode == "" {
		settings.mode = EqeqeqAlways
	}
	// `nullOption = config === "always" ? options.null || "always" : "ignore"`. The null policy is
	// only consulted under Always; Smart exempts every null comparison on its own, so a null policy
	// written beside Smart is discarded rather than honoured. Upstream's schema makes that
	// combination unwritable; ours cannot, so the discard happens here.
	if settings.mode != EqeqeqAlways {
		settings.null = EqeqeqNullIgnore
	} else if settings.null == "" {
		settings.null = EqeqeqNullAlways
	}
	return settings
}
