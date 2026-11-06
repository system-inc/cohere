package core

import "fmt"

// backreferenceProblem is one of the five ways a group cannot have run.
type backreferenceProblem struct {
	id     string
	reason string
}

// describe builds the finding's sentence from the reference and the group it names.
func (problem backreferenceProblem) describe(reference string, group string, otherGroups int) string {
	also := ""
	switch {
	case otherGroups == 1:
		also = " (and so does one other group of that name)"
	case otherGroups > 1:
		also = fmt.Sprintf(" (and so do %d other groups of that name)", otherGroups)
	}
	return fmt.Sprintf("The backreference %s can never match, because %s %s%s. It matches the empty "+
		"string instead of failing, so the pattern quietly matches more than it looks like it does.",
		reference, group, problem.reason, also)
}
