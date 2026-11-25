package react

// ButtonHasTypeOptions is the decoded option object.
//
// Three booleans, one per HTML button type, matching `meta.schema` exactly. All three default to
// `true`, which is the opposite of Go's zero value, and that inversion is the whole reason the decoder
// below is hand-written; see DecodeButtonHasTypeOptions.
type ButtonHasTypeOptions struct {
	// Button allows `type="button"`. Default true.
	Button bool

	// Submit allows `type="submit"`. Default true.
	Submit bool

	// Reset allows `type="reset"`. Default true.
	Reset bool
}

// allows reports whether a written-out type is permitted, and whether it is a type at all.
//
// The two returns are upstream's two branches in `checkValue` and they mean different things. The
// second says the value is not one of the three names in the configuration object at all, which is
// `invalidValue`; the first says it is one of them and is switched off, which is `forbiddenValue`.
// Upstream expresses both against the same object, so a name outside the three can never be
// "forbidden" no matter how the rule is configured.
func (options ButtonHasTypeOptions) allows(value string) (permitted bool, known bool) {
	switch value {
	case "button":
		return options.Button, true
	case "submit":
		return options.Submit, true
	case "reset":
		return options.Reset, true
	}
	return false, false
}
