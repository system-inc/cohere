// Package fixes_messaged holds calls only the port finds, each recorded with the text its fix writes, and the first
// with the fix's message too.
package fixes_messaged

func gained() {}

// calls holds the planted records, each ruled in this doc comment: the port rightly finds gained, and rightly fixes
// each call by writing fixed() in its place, under the message "write fixed()".
func calls() {
	gained() // port finds: call to gained // port fixes with "write fixed()": fixed()
	gained() // port finds: call to gained // port fixes: fixed()
}
