// Package dropped holds a value whose type is an alias of a type parameter, which the original skips when the type
// parameter is named as itself, since an instantiation may be a formatter, and reports through the alias.
package dropped

import "fmt"

func dropped[T fmt.Stringer](value T) string {
	type Alias = T
	var aliased Alias = value
	// The alias is the type parameter, which the original skips when it is named as itself.
	return fmt.Sprintf("%s", aliased) // port drops: should use String() instead of fmt.Sprintf
}

func named[T fmt.Stringer](value T) string {
	return fmt.Sprintf("%s", value)
}
