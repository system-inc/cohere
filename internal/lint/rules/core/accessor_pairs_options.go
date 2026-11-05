package core

// AccessorPairsOptions configures which half of the pairing this rule enforces, and where.
//
// Every field is a pointer because two of the four default to `true` upstream. The config layer hands
// a rule configured as a bare severity a nil, and a bool field behind a nil decodes to false, which
// would invert those two defaults silently. A pointer keeps "absent" and "written as false" apart,
// which is the whole reason the decoder below is hand-rolled rather than `rule.DecodeOptionsInto`.
type AccessorPairsOptions struct {
	// GetWithoutSet reports a getter with no matching setter. Defaults to `false` upstream.
	//
	// Off by default because a read-only property is an ordinary and intentional thing, while a
	// write-only one almost never is. The asymmetry is upstream's and is the reason the two options
	// exist separately rather than as one switch.
	GetWithoutSet *bool `json:"getWithoutSet"`

	// SetWithoutGet reports a setter with no matching getter. Defaults to `true` upstream.
	SetWithoutGet *bool `json:"setWithoutGet"`

	// EnforceForClassMembers extends the check to class bodies. Defaults to `true` upstream.
	EnforceForClassMembers *bool `json:"enforceForClassMembers"`

	// EnforceForTsTypes extends the check to interface bodies and type literals. Defaults to `false`.
	//
	// Off by default because a type declaring only `get prop(): T` is a legitimate way to describe a
	// readonly-through-an-accessor shape, and a setter with no getter in a type is far less
	// obviously a mistake than the same thing in a class.
	EnforceForTsTypes *bool `json:"enforceForTSTypes"`
}

// resolve applies each written option over the defaults, leaving an absent key alone.
func (options AccessorPairsOptions) resolve() accessorPairsSettings {
	settings := defaultAccessorPairsSettings()
	if options.GetWithoutSet != nil {
		settings.getWithoutSet = *options.GetWithoutSet
	}
	if options.SetWithoutGet != nil {
		settings.setWithoutGet = *options.SetWithoutGet
	}
	if options.EnforceForClassMembers != nil {
		settings.enforceForClassMembers = *options.EnforceForClassMembers
	}
	if options.EnforceForTsTypes != nil {
		settings.enforceForTsTypes = *options.EnforceForTsTypes
	}
	return settings
}
