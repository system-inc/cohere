//go:build !windows

package gate

// copy is the package's own, in every build but windows's.
func copy(destination []int, source []int) int { return 0 }
