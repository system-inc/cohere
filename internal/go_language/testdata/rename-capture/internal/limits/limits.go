// Package limits sizes a read. A file only windows builds declares maxBytes's new name as a local, in the scope of a
// use of maxBytes, so renaming it would make that use the local's, though windows still type-checks it.
package limits

// maxBytes is the most a read takes.
const maxBytes = 64

// Size is the most a read takes, with what the platform adds.
func Size() int {
	return maxBytes + extra()
}
