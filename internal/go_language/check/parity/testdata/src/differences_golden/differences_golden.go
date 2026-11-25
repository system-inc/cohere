// Package differences_golden is differences_fixed's source beside a golden file alone, the same text as that
// fixture's fixed file, which a comparison that formats first holds. It has no fixed file of its own, so the forms
// that refuse one read it.
package differences_golden

func shared() {}
func gained() {}
func fixed()  {}

// calls holds the planted records, each ruled in this doc comment: the port rightly finds gained, and rightly fixes
// it by writing fixed() in its place.
func calls() {
	gained() // port finds: call to gained // port fixes: fixed()
	shared()
}
