package react

// ForbidPropTypesOptions configures which prop types are refused and where they are looked for.
type ForbidPropTypesOptions struct {
	// Forbid is the list of prop type names to refuse. Absent means upstream's default of
	// `any`, `array` and `object`; an explicitly empty list forbids nothing, which is a
	// different thing and is why the wire field is a pointer.
	Forbid *[]string `json:"forbid"`

	// CheckContextTypes extends the rule to `contextTypes` declarations.
	CheckContextTypes bool `json:"checkContextTypes"`

	// CheckChildContextTypes extends the rule to `childContextTypes` declarations.
	CheckChildContextTypes bool `json:"checkChildContextTypes"`
}

// forbiddenTypes returns the effective forbid list.
func (options ForbidPropTypesOptions) forbiddenTypes() []string {
	if options.Forbid == nil {
		return forbidPropTypesDefaultForbid
	}
	return *options.Forbid
}
