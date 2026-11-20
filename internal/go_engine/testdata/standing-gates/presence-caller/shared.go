package gate

import "testing"

// Start takes a test and starts helper with go: it stands, and helper, which the tagged build reaches, does not.
func Start(t *testing.T) { go helper() }

// helper returns the tag's value.
func helper() int { return tagged() }

// Port starts nothing.
func Port() int { return 80 }
