// Package found_spanning holds calls only the port finds, each recorded with the column its finding starts at and
// where it ends, on its own line by column alone and by line and column.
package found_spanning

func gained() {}

// calls holds the planted records, each ruled in this doc comment: the port rightly finds gained, over the call.
func calls() {
	gained() // port finds at column 2 to 10: call to gained
	gained() // port finds at column 2 to 10:10: call to gained
}
