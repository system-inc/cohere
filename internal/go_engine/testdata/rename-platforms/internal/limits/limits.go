// Package limits sizes a read. Its maxBytes is read by a file only windows builds and one only freebsd builds, so a
// rename of it is proven under each of them too.
package limits

// maxBytes is the most a read takes.
const maxBytes = 64

// Size is the most a read takes, with what the platform adds.
func Size() int {
	return maxBytes + extra()
}
