package suppression

// Directive is one suppression comment, resolved to the lines and rules it covers.
type Directive struct {
	Kind Kind

	// Rules are the rule names the comment named. Empty means it named none, which silences
	// everything in its scope: a blanket, and the shape most worth being able to count.
	Rules []string

	// Reason is the text after ` -- `, trimmed. Empty means the author did not say why.
	Reason string

	// Line is the zero-based line the comment starts on.
	Line int

	// StartLine and EndLine are the inclusive zero-based line span this directive covers.
	StartLine int
	EndLine   int

	// Position and End are the comment's own byte range, so an unused directive can be reported where it
	// actually sits rather than at the finding it failed to silence.
	Position int
	End      int

	// applied counts the findings this directive actually withheld.
	applied int

	// appliedRules records the registry names whose findings this comment withheld.
	appliedRules map[string]bool
}

// HasReason is whether the author said why.
func (directive *Directive) HasReason() bool {
	return directive.Reason != ""
}

// Covers is whether this directive applies to ruleName on a zero-based line.
//
// A directive naming no rules covers every rule; one naming rules covers only those, matched by
// plugin-qualified suffix so `nexus/consistency-no-enum` in a comment resolves against the bare
// `consistency-no-enum` a rule registers under. The corpus writes the plugin prefix and cohere's
// registry does not, and reconciling that here is cheaper than making every rule carry a prefix it
// has no other use for.
func (directive *Directive) Covers(ruleName string, line int) bool {
	if line < directive.StartLine || line > directive.EndLine {
		return false
	}
	if len(directive.Rules) == 0 {
		return true
	}
	for _, named := range directive.Rules {
		if matchesRuleName(named, ruleName) {
			return true
		}
	}
	return false
}

// Names is whether this directive could silence ruleName on some line: it names that rule, or names none.
func (directive *Directive) Names(ruleName string) bool {
	if len(directive.Rules) == 0 {
		return true
	}
	for _, named := range directive.Rules {
		if matchesRuleName(named, ruleName) {
			return true
		}
	}
	return false
}

// AppliedTo reports whether this directive withheld a finding for the named rule, resolving
// its spelling just as Covers does. A live name must survive removal of a dead sibling name.
func (directive *Directive) AppliedTo(named string) bool {
	for registered := range directive.appliedRules {
		if matchesRuleName(named, registered) {
			return true
		}
	}
	return false
}
