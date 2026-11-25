// Package found_at_column holds calls only the port finds, each recorded with the column its finding starts at, and
// one recorded without, matched by its line and message alone.
package found_at_column

func gained() int { return 0 }

// calls holds the planted records, each ruled in this doc comment: the port rightly finds gained, at the call.
func calls() {
	gained()     // port finds at column 2: call to gained
	_ = gained() // port finds at column 6: call to gained
	_ = gained() // port finds: call to gained
}
