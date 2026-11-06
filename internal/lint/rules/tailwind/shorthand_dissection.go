package tailwind

// shorthandDissection is one class split into the parts a collapse has to match on.
type shorthandDissection struct {
	className string
	variants  string
	base      string
	negative  bool
	important bool
	markerEnd bool
	// matches is every table pattern the base matches, with what each captured.
	matches []shorthandMatch
}

// match reports whether this class matches one pattern, and what it captured.
func (dissection shorthandDissection) match(pattern int) (string, bool) {
	for _, candidate := range dissection.matches {
		if candidate.pattern == pattern {
			return candidate.capture, true
		}
	}
	return "", false
}
