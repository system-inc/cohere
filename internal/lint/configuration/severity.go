package configuration

// Severity is whether a rule runs, and how loudly.
type Severity int

// String renders a severity the way the config spells it.
//
// Without this, fmt prints the underlying int as a control character, because Severity is an
// iota and SeverityOff is 0. A diagnostic about a config key that rendered its severity as
// '\x00' would be describing the exact problem it exists to report, illegibly.
func (severity Severity) String() string {
	switch severity {
	case SeverityOff:
		return "off"
	case SeverityWarn:
		return "warn"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}
