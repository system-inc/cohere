// Package panicking_renamed holds a call the planted original panics on, and a renaming, which has no finding of the
// original's there to rename.
package panicking_renamed

func panics()  {}
func renamed() {}

// calls holds the planted record, ruled in this doc comment: the port renames renamed.
func calls() {
	panics()
	renamed() // port renames: call to renamed in the original's words => call to renamed in the port's words
}
