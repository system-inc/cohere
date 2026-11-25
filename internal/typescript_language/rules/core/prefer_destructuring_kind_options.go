package core

// PreferDestructuringKindOptions is the per-node-kind pair of switches.
type PreferDestructuringKindOptions struct {
	// Array enables the array-index arm, e.g. `var foo = array[0]`.
	Array *bool `json:"array"`
	// Object enables the property arm, e.g. `var foo = object.foo`.
	Object *bool `json:"object"`
}

// enabled reads one switch, where an absent switch means off.
//
// Upstream's `shouldCheck` is `normalizedOptions[nodeType][destructuringType]`, a plain truthiness
// test, so an absent key is falsy and the arm is off. Only the wholly-default configuration turns
// everything on, which the decoder builds explicitly.
func (options PreferDestructuringKindOptions) enabled(array bool) bool {
	switch {
	case array:
		return options.Array != nil && *options.Array
	default:
		return options.Object != nil && *options.Object
	}
}
