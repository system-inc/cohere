package core

// NoUnneededTernaryOptions configures the rule.
type NoUnneededTernaryOptions struct {
	// DefaultAssignment allows `foo ? foo : bar`, the pre-`||` way to spell a default.
	//
	// Defaults to `true` upstream, so the second half of this rule is off unless somebody asks for
	// it. A pointer because of that default: the config layer hands a rule configured as a bare
	// severity a nil, and a plain bool field behind a nil decodes to false, which would silently
	// turn the second judgment on for everyone.
	DefaultAssignment *bool `json:"defaultAssignment"`
}

// resolve applies the default, leaving an absent key alone.
func (options NoUnneededTernaryOptions) resolve() bool {
	if options.DefaultAssignment == nil {
		return noUnneededTernaryDefaultAssignment
	}
	return *options.DefaultAssignment
}
