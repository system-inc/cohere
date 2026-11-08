// Package adjudicated holds a finding only the port makes and one both make in different words, each recorded.
package adjudicated

func shared() {}

func gained() {}

func renamed() {}

func calls() {
	shared()
	gained()  // port finds: call to gained
	renamed() // port renames: call to renamed in the original's words => call to renamed in the port's words
}
