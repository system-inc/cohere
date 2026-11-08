// Package fixed holds calls only the port finds, each recorded with the text the port's fix writes in its place, once
// on the line of its `finds:` record and once on the line after it, written as a Go string.
package fixed

func gained() {}

// calls holds the planted records, each ruled in this doc comment: the port rightly finds gained, and rightly fixes
// each call by writing fixed() in its place.
func calls() {
	gained() // port finds: call to gained // port fixes: fixed()
	gained() // port finds: call to gained
	// port fixes: "fixed()"
}
