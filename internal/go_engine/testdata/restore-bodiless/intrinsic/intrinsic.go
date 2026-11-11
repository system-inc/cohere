// Package intrinsic declares Raw without a body, as a package of assembly or a compiler intrinsic does, whose stub.s
// makes it a package go build takes; cohere never assembles it. Every build declares it alike.
package intrinsic

// Raw is a value from below Go, whose body is not Go.
func Raw(value int) int
