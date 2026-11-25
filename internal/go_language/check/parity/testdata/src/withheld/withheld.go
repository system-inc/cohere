// Package withheld holds a call both sides find in the same words, recorded as one where the original offers a fix
// to delete it that the port leaves out.
package withheld

func shared() {}

func calls() {
	// The original's fix is ruled one the port rightly leaves out.
	shared() // port withholds: delete the call
}
