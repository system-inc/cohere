//go:build !windows

package gate

// Bar is a method of Foo in every build but windows's.
func (Foo) Bar() {}
