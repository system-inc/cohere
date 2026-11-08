// Package tagged holds a slice of a type defined on byte, which converts to a string from go1.18's language on, in a
// go1.17 module: this file under the module's version, where neither side reports it, and tagged_newer.go under a
// version of its own.
package tagged

import "fmt"

type Byte byte

func older(bytes []Byte) {
	_ = fmt.Sprintf("%s", bytes)
}
