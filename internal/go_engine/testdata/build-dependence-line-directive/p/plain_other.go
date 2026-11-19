//go:build !windows && !freebsd

package p

// Plain is in a file only the host compiles, read after gen_other.go, and holds no line directive.
func Plain() int { return 6 }
