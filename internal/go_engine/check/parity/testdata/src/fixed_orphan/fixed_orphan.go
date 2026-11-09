// Package fixed_orphan holds a call only the port finds and one both find, each fixed by the port, beside a fixed
// file named for a source that is not here, as one whose source was renamed is, which nothing would read.
package fixed_orphan

func shared() {}
func gained() {}
func fixed()  {}

// calls holds the planted records, each ruled in this doc comment: the port rightly finds gained, and rightly fixes
// it by writing fixed() in its place.
func calls() {
	gained() // port finds: call to gained // port fixes: fixed()
	shared()
}
