// Package unmatched records differences that do not happen: both sides find each call here in the same words.
package unmatched

func gained() {}
func shared() {}

// calls holds records ruled here, which the findings do not bear out.
func calls() {
	gained() // port finds: call to gained
	shared() // port finds: call to shared
	shared() // port renames: call to shared => call to shared in other words
}
