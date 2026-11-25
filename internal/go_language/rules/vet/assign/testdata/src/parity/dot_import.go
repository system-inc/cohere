package parity

import . "os"

// A package variable reached through a dot import, which prints without its qualifier.
func dotImported() {
	Args = Args       // self-assignment
	Args[0] = Args[0] // self-assignment
	Args = Environ()
}
