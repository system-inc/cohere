// Package constants holds constants of every kind and expressions that are near misses of each: boolean constants
// typed, untyped, named and aliased, integer and string constants by literal, by name and by expression, integers
// too large for an int64, signed and parenthesized literals, and nil in every place it can stand.
package constants

import "unsafe"

type flag bool

type boolAlias = bool

type enumeration int

const (
	yes                      = true
	no           bool        = false
	flagged      flag        = true
	aliased      boolAlias   = true
	expressed                = 1 < 2
	answer                   = 42
	typedAnswer  int         = 42
	huge                     = 1 << 70
	negativeHuge             = -1 << 70
	name                     = "constants"
	typedName    string      = "typed"
	ratio                    = 1.5
	whole                    = 2.0
	imaginary                = 2i
	letter                   = 'a'
	first        enumeration = iota
	second
)

var (
	variable      = true
	integer       = 7
	text          = "text"
	pointer  *int = nil
	big           = uint64(1 << 63)
)

func booleans() {
	_ = yes == true
	_ = no != false
	_ = flagged == true
	_ = aliased == false
	_ = expressed
	_ = variable == yes
	_ = !yes
	_ = (yes)
	_ = true && false
	const local = false
	_ = local
	_ = first == second
}

func integers() []any {
	return []any{
		0, 1, -1, +1, - -1, -(1), (-1), (5), 0x10, 0o17, 0b101, 1_000, 'a', 1.0, 2.5, 1e3, 2i,
		answer, typedAnswer, -answer, answer + 1, 1 + 2, huge >> 10, huge > 0, negativeHuge < 0,
		int8(-128), uint8(255), big, uint64(18446744073709551615), int64(-9223372036854775808),
		len("four"), len([3]int{}), unsafe.Sizeof(integer), cap([2]int{}), integer, -integer,
		ratio, whole, imaginary, letter, first, second, enumeration(3), ^0, ^uint(0), 1 << 3,
		9223372036854775807, -9223372036854775808,
	}
}

func strings() []any {
	return []any{
		"", "literal", `raw`, name, typedName, name + "!", "a" + "b", text, text + name, string('x'),
		string(rune(65)), len(name),
	}
}

func nils(values map[string]int, function func()) []any {
	var channel chan int
	var slice []int
	var anything any
	_ = slice == nil
	_ = values != nil
	_ = function == nil
	_ = channel == (nil)
	_ = anything == nil
	_ = pointer
	_ = (*int)(nil)
	_ = []int(nil)
	return []any{nil, (nil), (nil), pointer, anything, slice}
}
