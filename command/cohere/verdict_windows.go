package main

import "os"

// takeVerdictFile listens for nothing on Windows, where a child cannot inherit a descriptor beyond its
// standard three, so the dispatcher there waits for the engine as it always did.
func takeVerdictFile() *os.File {
	// Ignored: Unsetenv fails only for a name holding "=" or a NUL, and VerdictVariable holds neither.
	_ = os.Unsetenv(VerdictVariable)
	return nil
}
