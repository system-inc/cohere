package staticcheck_wants

import (
	"fmt"
	"regexp"
	"strings"
)

// FactWant is one fact a fixture of staticcheck's expects an analyzer to export about an object, written
// `//@ fact(name, "text")` on the line the object is declared on, read by ReadWithFacts.
type FactWant struct {
	// File is the full path of the fixture file the note is in.
	File string
	// Line is the line of the note, the line the object's declaration must be on.
	Line int
	// Object is the name of the object the fact is about, the note's first argument.
	Object string
	// Text is what the fact, printed, must contain, when Pattern is nil.
	Text string
	// Pattern is what the fact, printed, must match, for a note written re"...".
	Pattern *regexp.Regexp
}

// Matches is whether fact, printed as staticcheck's runner prints it, is the fact want expects about its object, as
// staticcheck's testutil judges it: it contains Text, or matches Pattern.
func (want FactWant) Matches(fact string) bool {
	if want.Pattern != nil {
		return want.Pattern.MatchString(fact)
	}
	return strings.Contains(fact, want.Text)
}

// String is want as its fixture writes it, after its file and line.
func (want FactWant) String() string {
	if want.Pattern != nil {
		return fmt.Sprintf("%s:%d: fact(%s, re%q)", want.File, want.Line, want.Object, want.Pattern)
	}
	return fmt.Sprintf("%s:%d: fact(%s, %q)", want.File, want.Line, want.Object, want.Text)
}
