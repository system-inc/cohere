// Package fixes_unanchored holds a `fixes:` record two lines below the `finds:` record it means, so it follows none.
package fixes_unanchored

func gained() {}

// calls holds the planted records, each ruled in this doc comment: the port rightly finds gained, and the fix
// recorded below it is too far from it to be its.
func calls() {
	gained() // port finds: call to gained

	// port fixes: fixed()
}
