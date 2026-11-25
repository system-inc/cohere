// Package module_adjudicated is the differences fixture as a module: a finding only the port makes and one both make
// in different words, each recorded.
package module_adjudicated

func shared() {}

func gained() {}

func renamed() {}

// calls holds the planted records, each ruled in this doc comment: the port rightly finds gained and renames renamed.
func calls() {
	shared()
	gained()  // port finds: call to gained
	renamed() // port renames: call to renamed in the original's words => call to renamed in the port's words
}
