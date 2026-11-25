// Package differences_fixed holds a call only the port finds and one both find, each fixed by the port, beside its
// fixed file, this file as every fix the port offers in it leaves it, byte for byte. differences_golden holds the
// same source beside a golden file instead.
package differences_fixed

func shared() {}
func gained() {}
func fixed()  {}

// calls holds the planted records, each ruled in this doc comment: the port rightly finds gained, and rightly fixes
// it by writing fixed() in its place.
func calls() {
	gained() // port finds: call to gained // port fixes: fixed()
	shared()
}
