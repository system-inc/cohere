// Package shadowed declares byte and string at package scope, so neither names the universe's type here.
package shadowed

import "bytes"

// byte is uint8 spelled again, the universe's uint8, a type distinct from the universe's byte.
type byte = uint8

// string is a function, so a call of it is no conversion.
var string = func(contents []uint8) []uint8 { return contents }

func conversions(buffer *bytes.Buffer) {
	_ = []byte(buffer.String())
	_ = string(buffer.Bytes())
}
