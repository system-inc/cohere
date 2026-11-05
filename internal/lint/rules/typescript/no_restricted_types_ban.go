package typescript

// NoRestrictedTypesBan is one entry's resolved meaning.
type NoRestrictedTypesBan struct {
	// Allowed records an entry the configuration wrote as `false` or `null`, meaning "not banned".
	//
	// Upstream's rule tests `bannedType == null || bannedType === false` and declines, so the
	// behavior is real in its source. Its schema rejects both spellings before the rule can see
	// them: measured against the installed 8.67.0 build, `{"Foo": null}` and `{"Foo": false}` both
	// fail configuration validation and the linter refuses to start. So this branch reproduces
	// upstream's code and is unreachable through upstream's own configuration surface. It is kept
	// because cohere has no schema layer rejecting those spellings, which means a config here can
	// contain one, and silently banning a type somebody wrote `false` against would be the worst
	// available reading.
	Allowed bool

	// Message is the text appended after the standard sentence, from either the string form or the
	// object's `message` key.
	Message string

	// FixWith produces a fix the edit engine applies unattended.
	//
	// A plain string rather than a pointer, deliberately, because upstream cannot distinguish an
	// absent key from an empty one either: it tests the value's truthiness, so `fixWith: ""` means
	// no repair rather than "replace with nothing". Measured on the installed 8.67.0 build, which
	// reports that case with no fix attached. A pointer here would express a distinction upstream
	// does not have and would delete the type.
	FixWith string

	// Suggest produces suggestions a human chooses between.
	Suggest []string
}

// customMessage renders the trailing half of the finding.
//
// Upstream returns the empty string for a bare `true` and otherwise a space followed by the message,
// so the standard sentence and the custom one are separated by exactly one space and a bare ban ends
// at the period.
func (ban NoRestrictedTypesBan) customMessage() string {
	if ban.Message == "" {
		return ""
	}
	return " " + ban.Message
}
