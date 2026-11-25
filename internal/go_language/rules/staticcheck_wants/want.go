package staticcheck_wants

import (
	"fmt"
	"regexp"
	"strings"
)

// Want is one finding a fixture of staticcheck's expects, written `//@ diag(...)` on the line the finding is
// reported on.
type Want struct {
	// File is the full path of the fixture file the note is in.
	File string
	// Line is the line of the note, the line the finding's start must be on.
	Line int
	// Text is what the finding's message must contain, when Pattern is nil.
	Text string
	// Pattern is what the finding's message must match, for a note written re"...".
	Pattern *regexp.Regexp
}

// Matches is whether message is the message of a finding want expects, as staticcheck's testutil judges it: it
// contains Text, or matches Pattern.
func (want Want) Matches(message string) bool {
	if want.Pattern != nil {
		return want.Pattern.MatchString(message)
	}
	return strings.Contains(message, want.Text)
}

// String is want as its fixture writes it, after its file and line.
func (want Want) String() string {
	if want.Pattern != nil {
		return fmt.Sprintf("%s:%d: diag(re%q)", want.File, want.Line, want.Pattern)
	}
	return fmt.Sprintf("%s:%d: diag(%q)", want.File, want.Line, want.Text)
}
