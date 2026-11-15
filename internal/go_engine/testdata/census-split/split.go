// Package split declares one function every build compiles and one each build declares in a file of its own.
package split

// Shared is the same code in every build, its parameter's answer the same object's in each.
func Shared(dst []byte) []byte { return append(dst, 's') }
