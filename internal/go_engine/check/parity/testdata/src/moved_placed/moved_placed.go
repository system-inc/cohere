// Package moved_placed holds calls both sides find in other spans, recorded as moved, the records placing the port's
// finding, naming the message of its fix, or both.
package moved_placed

func shared() {}

// calls holds the planted records, each ruled in this doc comment: the port's spans and fixes are ruled right.
func calls() {
	shared() // port moves at column 2 to 9 with "write fixed()": call to shared
	shared() // port moves at column 2: call to shared
	shared() // port moves with "write fixed()": call to shared
}
