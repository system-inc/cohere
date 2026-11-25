// Package universe declares a string of its own, which every file of the package sees in the predeclared type's
// place.
package universe

import (
	"fmt"
	"reflect"
)

var string = 0

func universe(tag reflect.StructTag, raw []byte) {
	// The package's string is an int, so the original's `string(tag)` calls no conversion.
	_ = fmt.Sprintf("%s", tag) // port withholds: Replace with conversion to string
	// Nor does its `string(raw)`.
	_ = fmt.Sprintf("%s", raw) // port withholds: Replace with conversion to string
}
