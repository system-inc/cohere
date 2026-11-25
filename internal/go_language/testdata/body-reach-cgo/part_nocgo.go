//go:build !cgo

package gate

// Other is windows's, a build without cgo, whose file imports nothing.
func Other() int { return 0 }
