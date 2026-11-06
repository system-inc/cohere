package core

// NoSequencesOptions configures whether an explicitly parenthesized sequence is excused.
type NoSequencesOptions struct {
	// AllowInParentheses is upstream's only option and it defaults to `true`, which is why it is a
	// pointer rather than a bool.
	//
	// A zero-value struct would read as `false` and silently invert the rule: every `var foo = (1,
	// 2)` in the tree would report, and no fixture built by handing `RunWithOptions` a struct could
	// see it, because such a fixture states the value it is testing. The nil case is the one the
	// live config actually produces, since a rule configured as bare "error" is handed no options at
	// all, and it is the case the default has to survive.
	AllowInParentheses *bool `json:"allowInParentheses"`
}

// allowInParentheses reads the option, supplying upstream's default for an absent or nil value.
func (options NoSequencesOptions) allowInParentheses() bool {
	if options.AllowInParentheses == nil {
		return true
	}
	return *options.AllowInParentheses
}
