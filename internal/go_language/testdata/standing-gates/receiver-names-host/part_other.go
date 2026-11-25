//go:build !windows

package gate

// C names its receiver otherwise, in every build but windows's.
func (thing Thing) C() {}
