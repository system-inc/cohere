// Package reads counts reads. Its maxReads is read by a cgo file only windows builds, which no load here reads.
package reads

// maxReads is the most reads a call makes.
const maxReads = 4

// Count is the most reads a call makes.
func Count() int {
	return maxReads
}
