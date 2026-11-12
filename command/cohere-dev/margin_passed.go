package main

import "fmt"

// marginPassed is the error of a command whose bounded run's process group held more than its margin (bound.go).
type marginPassed struct {
	name   string
	margin int
	held   int
}

func (passed *marginPassed) Error() string {
	return fmt.Sprintf("%s's process group held %d processes, past the bounded run's margin of %d, so the group "+
		"was stopped", passed.name, passed.held, passed.margin)
}
