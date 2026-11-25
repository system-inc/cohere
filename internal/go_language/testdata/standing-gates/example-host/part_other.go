//go:build !windows

package gate

// Foo is declared in every build but windows's.
func Foo() {}
