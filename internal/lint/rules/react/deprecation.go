package react

import "fmt"

// deprecation is one row of upstream's table.
//
// `since` is printed and decides nothing, for the reason in the rule's doc comment. `replacement`
// and `reference` are empty when upstream's array is shorter than three, and each empty one drops
// its whole clause from the message rather than printing an empty one.
type deprecation struct {
	since       string
	replacement string
	reference   string
}

// describe renders the finding exactly as upstream's message template does.
//
// The template is `{{oldMethod}} is deprecated since React {{version}}{{newMethod}}{{refs}}`, where
// `newMethod` is `, use X instead` or the empty string and `refs` is `, see Y` or the empty string.
// Those two conditionals live in the report call upstream rather than in the template, so a port
// interpolating four raw slots would print a stray comma on the two entries that carry no
// replacement. `React.isValidClass` and `React.addons.LinkedStateMixin` are those two, and both are
// pinned by a fixture asserting this whole string.
func (entry deprecation) describe(oldMethod string) string {
	description := fmt.Sprintf("%s is deprecated since React %s", oldMethod, entry.since)
	if entry.replacement != "" {
		description += ", use " + entry.replacement + " instead"
	}
	if entry.reference != "" {
		description += ", see " + entry.reference
	}
	return description
}
