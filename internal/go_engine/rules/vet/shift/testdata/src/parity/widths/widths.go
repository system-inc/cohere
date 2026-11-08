// Package widths is imported by parity, plainly and with a dot, for constants, types and constraints declared in
// another package.
package widths

import "unsafe"

// Word is the width in bits of an int on the target.
const Word = 8 * unsafe.Sizeof(int(0))

// Byte is the width in bits of a byte.
const Byte = 8

// Small is a named integer type of 16 bits.
type Small int16

// Narrow is a constraint whose type set holds widths of 8 and 16 bits.
type Narrow interface {
	~int8 | ~uint16
}

// Value is a struct whose field is shifted from another package.
type Value struct {
	Field uint8
}
