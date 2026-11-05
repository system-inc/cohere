package typescript

// ArrayTypeOptions is the rule's configuration.
//
// Upstream's schema is one object with `default` and `readonly`, both drawn from the three
// spellings above, `additionalProperties: false`, and `defaultOptions` of `{ default: 'array' }`.
type ArrayTypeOptions struct {
	// Default governs a mutable array and is upstream's `default`, whose own default is `array`.
	Default ArrayTypeSetting

	// Readonly governs `readonly T[]`, `ReadonlyArray<T>` and `Readonly<T[]>`. Upstream's `readonly`
	// is optional and falls back to `default` rather than to a fixed value, so this field holds the
	// resolved answer and the decoder does the falling back.
	Readonly ArrayTypeSetting
}

// settingFor picks the axis a node is judged on.
func (options ArrayTypeOptions) settingFor(isReadonly bool) ArrayTypeSetting {
	if isReadonly {
		return options.Readonly
	}
	return options.Default
}
