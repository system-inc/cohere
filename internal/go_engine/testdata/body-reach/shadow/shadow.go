// Package shadow holds the body reach's shadowed names (#w72zxtq): windows declares len and byte at package level in
// place of the universe's, which reaches the package whole as well (ScopeName, #b255cfy), so they live apart from
// package reach, whose bodies the census judges one by one.
package shadow

// Size reads the universe's len, which windows's file declares at package level.
func Size(values []int) int {
	return len(values)
}

// Bytes takes a byte, which windows declares at package level in place of the universe's.
func Bytes(value byte) int {
	return 0
}
