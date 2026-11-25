package parity

import "fmt"

// Format does what fmt.Sprintf does but is parity's own, reported only when funcs names parity.Format. It cannot share
// fmt.Sprintf's name, which dot_fmt.go dot imports.
func Format(format string, arguments ...any) string { return fmt.Sprintf(format, arguments...) }

// Identity and Both are generic, reported only when funcs names them.
func Identity[T any](value T) T { return value }

func Both[K comparable, V any](key K, value V) (K, V) { return key, value }

// Unlisted is never named.
func Unlisted() string { return "" }

// Pure is a method, which funcs never names: an entry of funcs is split at its last dot into a package path and a
// name, and only a function without a receiver is looked up.
func (shapes) Pure() string { return "" }

// Under parity's own lists, these are reported where noted, and of the defaults only errors.New stays judged.
func configured() {
	var shaped shapes
	Format("")               // reported when configured
	Identity(1)              // reported when configured
	Identity[string]("")     // reported when configured
	(Identity[int])(1)       // reported when configured
	Both(1, "")              // reported when configured
	Both[int, string](1, "") // reported when configured
	Unlisted()               // clean
	shaped.Pure()            // clean under funcs; String-typed, so judged by stringmethods alone
	shaped.Name()            // reported when configured
	shaped.GoString()        // reported when configured
	shaped.Count()           // clean: not of type func() string
	identity := Identity[int]
	identity(1) // clean
}
