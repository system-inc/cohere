package differential

import "errors"

// refuser handles nothing, which is what a native printer looks like before it exists.
type refuser struct{}

func (refuser) Handles(string) bool { return false }

func (refuser) Format(string, string) (string, error) { return "", errors.New("unreachable") }
