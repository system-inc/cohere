//go:build !windows

package gate

// shape is a marker interface, declared in every build but windows's.
type shape interface{ isShape() }
