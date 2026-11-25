package dotted

import . "time"

// The file imports time into its own scope alone, so the original's time.Since names nothing, and the port writes
// Since, which is time's in this file.
func dotted(t Time) Duration {
	// The callee is time.Now by the dot import, and so is Since.
	_ = (Now().Sub)(t) // port moves: should use time.Since instead of time.Now().Sub
	// The same without the callee's parentheses.
	return Now().Sub(t) // port moves: should use time.Since instead of time.Now().Sub
}

// A local Since shadows time's, so no name the fix could write calls time.Since here, and the finding stays alone.
func shadowed(t Time) {
	Since := t
	// Neither time nor Since names time's function at the call.
	_ = Now().Sub(Since) // port withholds: Replace with call to time.Since
}
