package gate

import "testing"

// Start takes a test and starts helper with go, in windows's build alone.
func Start(t *testing.T) { go helper() }
