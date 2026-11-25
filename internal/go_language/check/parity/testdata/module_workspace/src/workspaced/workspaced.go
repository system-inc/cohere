// Package workspaced is the differences fixture as a module a testdata's go.work uses from below its src, as
// time_until's differences module sits: the go command finds it there by its import path, so its records sit in the
// folder the load found it in, never at testdata/workspaced.
package workspaced

func shared() {}

func gained() {}

func renamed() {}

// calls holds the planted records, each ruled in this doc comment: the port rightly finds gained and renames renamed.
func calls() {
	shared()
	gained()  // port finds: call to gained
	renamed() // port renames: call to renamed in the original's words => call to renamed in the port's words
}
