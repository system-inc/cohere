// Package omitted holds a call both sides find in the same words, recorded as one where the original relates the
// function called and the port leaves that related entry out.
package omitted

func shared() {}

func calls() {
	// The original's related entry is ruled one the port rightly leaves out.
	shared() // port omits: the function called
}
