//go:build !windows || !cgo

// Package limits sizes a read. Its maxBytes has a twin in a cgo file only windows builds, which no load here reads,
// since every load runs with cgo off.
package limits

// maxBytes is the most a read takes.
const maxBytes = 64

// Size is the most a read takes.
func Size() int {
	return maxBytes
}
