// Package despite holds calls both sides find beside two type errors, and its test file holds a third, so the
// comparison runs over what of it type-checked, and its test variant reports the first two again.
package despite

func shared() {}

func calls() {
	shared()
	shared()
}

var count int = "many"

var other int = "many"
