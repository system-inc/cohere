// Package facts holds a function that ends the process on windows alone, through a function each platform's file
// declares, one that ends it everywhere with a code windows declares apart, and one whose value differs on windows,
// each called from this shared file.
package facts

import "os"

// Finish ends the process on windows and returns everywhere else.
func Finish() {
	platformFinish()
}

// Run calls Finish, so it ends the process wherever Finish does.
func Run() {
	Finish()
}

// Width is twice the platform's size.
func Width() int {
	return size * 2
}

// Doubled is twice Width.
func Doubled() int {
	return Width() * 2
}

// Stop ends the process with the platform's code, so its body reads a constant windows declares apart while it never
// returns in any build.
func Stop() {
	os.Exit(code)
}

// Halt calls Stop, so it never returns in any build either.
func Halt() {
	Stop()
}
