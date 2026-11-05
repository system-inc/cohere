package regexsyntax

// RegexFlags captures the subset of ECMAScript regex flags that affects how
// patterns (and in particular character classes) are parsed.
type RegexFlags struct {
	Unicode     bool // u flag
	UnicodeSets bool // v flag
}

// UnicodeMode reports whether unicode/unicodeSets mode is active (either u or v).
func (flags RegexFlags) UnicodeMode() bool { return flags.Unicode || flags.UnicodeSets }
