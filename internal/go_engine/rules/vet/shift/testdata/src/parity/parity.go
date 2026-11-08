// Package parity holds every branch of shift's judgment, a violation and a clean case beside each, and the shapes
// most likely to differ between the original and the port. A comment says what each line is; the parity test
// compares the two analyzers' findings, so no line here carries a want.
package parity

import (
	"runtime"
	"unsafe"

	"parity/widths"
	. "parity/widths"
)

// Every width, signed and unsigned, at the edge and one past it, by shift and by shift assignment.
func Widths() {
	var signed8 int8
	signed8 = signed8 << 7 // clean, one below the width
	signed8 = signed8 << 8 // reported, the width
	signed8 = signed8 >> 9 // reported, past the width
	signed8 <<= 7          // clean
	signed8 >>= 8          // reported
	var unsigned16 uint16
	_ = unsigned16 << 15 // clean
	_ = unsigned16 >> 16 // reported
	unsigned16 <<= 100   // reported, far past
	var unsigned32 uint32
	_ = unsigned32 << 31 // clean
	_ = unsigned32 << 32 // reported
	var signed64 int64
	_ = signed64 >> 63        // clean
	_ = signed64 >> 64        // reported
	_ = signed8 << unsigned16 // clean, a count that is not constant
	_ = signed64 << 0         // clean
}

// int and uintptr, whose widths are the target's.
func TargetWidths() {
	var integer int
	var pointer uintptr
	var unsigned uint
	const integerBits = 8 * unsafe.Sizeof(integer)
	const pointerBits = 8 * unsafe.Sizeof(pointer)
	_ = integer << (integerBits - 1)               // clean
	_ = integer << integerBits                     // reported
	_ = pointer >> (pointerBits - 1)               // clean
	_ = pointer >> pointerBits                     // reported
	pointer <<= pointerBits                        // reported
	_ = unsigned << 64                             // reported on a 64 bit target, and on a 32 bit one
	_ = integer << 32                              // reported only on a 32 bit target
	_ = pointer << 32                              // reported only on a 32 bit target
	_ = integer << Word                            // reported, a count from a dot import
	_ = integer << widths.Word                     // reported, a count from a qualified import
	_ = pointer << (Byte * unsafe.Sizeof(pointer)) // reported
}

// Counts of every constant kind, and counts too large for an int64.
func Counts() {
	var value uint8
	_ = value << 8.0      // reported, a float count that is an integer
	_ = value << 'a'      // reported, a rune count
	_ = value << (4 + 4)  // reported, a constant expression
	_ = value << (1 << 2) // clean, a constant expression under the width
	const large = 1 << 62
	_ = value << large       // reported
	_ = value >> (large * 3) // clean, a count beyond an int64, which Int64Val refuses
	const typed uint64 = 1<<64 - 1
	_ = value << typed // clean, a count beyond an int64
}

// Constants shifted are left alone, however far.
func Constants() {
	const oneIf64Bit = ^uint(0) >> 63 // clean
	const wide = 1 << 100             // clean
	var count uint
	_ = 1 << count   // clean, the shifted operand is a constant
	_ = int8(1) << 6 // clean, a typed constant shifted
	_ = oneIf64Bit + wide>>100
}

// Named types, aliases, and types declared in another package.
type Bits uint16

type Alias = int8

type BitsAlias = Bits

func Named() {
	var bits Bits
	_ = bits << 15 // clean
	_ = bits << 16 // reported
	var aliased Alias
	_ = aliased << 8 // reported, through an alias
	var twice BitsAlias
	twice <<= 16 // reported, through an alias of a named type
	var small Small
	_ = small << 16 // reported, a type from a dot import
	var qualified widths.Small
	_ = qualified >> 16 // reported
}

// Operands of every shape, each printed as the message names it.
type holder struct {
	field   uint8
	array   [2]uint16
	pointer *uint32
	widths.Value
}

func (holder) method() int8 { return 1 }

func Operands(values []uint8, table map[string]uint16, value holder) {
	_ = values[0] << 8         // reported, an index
	table["key"] <<= 16        // reported, a map element assigned
	_ = value.field << 8       // reported, a field
	_ = value.Field << 8       // reported, an embedded field from another package
	_ = value.Value.Field >> 8 // reported, the same field reached in full
	_ = value.array[1] << 16   // reported
	_ = *value.pointer << 32   // reported, a dereference
	*value.pointer >>= 32      // reported
	_ = value.method() << 8    // reported, a method call
	method := value.method
	_ = method() << 8                     // reported, a method value called
	_ = holder.method(value) << 8         // reported, a method expression called
	_ = (values[0] + 1) << 8              // reported, a parenthesized sum
	_ = uint8(len(values)) << 8           // reported, a conversion
	_ = func() int16 { return 1 }() << 16 // reported, a closure called
	_ = func() int16 {
		return 1
	}() >> 16 // reported, a closure over lines, printed as the printer prints it
	_ = values[0] << 7 // clean
}

// Shifts inside closures and nested expressions.
func Closures() func() uint8 {
	var captured uint8
	return func() uint8 {
		captured <<= 8                           // reported, inside a closure
		return (captured << 1) | (captured << 8) // reported once, the second
	}
}

// Type parameters: one width, several widths, uintptr and int beside each other, constraints named, embedded,
// intersected, and from another package.
type Wide interface{ ~int32 | ~int64 }

type Mixed interface{ ~int8 | ~int64 }

func OneWidth[Signed int8 | uint8, Same ~int16 | ~uint16]() {
	var signed Signed
	_ = signed << 7 // clean
	_ = signed << 8 // reported, 8 bits
	var same Same
	same <<= 16 // reported, 16 bits, without may be
}

func SeveralWidths[Several ~int8 | ~int16 | ~int64, FromWide Wide]() {
	var several Several
	_ = several << 7  // clean, below the narrowest
	_ = several << 8  // reported, may be 8 bits
	_ = several >> 63 // reported, may be 8 bits
	several >>= 16    // reported
	var fromWide FromWide
	_ = fromWide << 31 // clean
	_ = fromWide << 32 // reported, may be 32 bits
}

func TargetTypeParameters[PointerOrInteger uintptr | int, PointerOrNarrow uintptr | int16, Words ~uint | ~uintptr | ~int]() {
	var pointerOrInteger PointerOrInteger
	_ = pointerOrInteger << 31 // clean
	_ = pointerOrInteger << 64 // reported, both 64 bits on a 64 bit target, so without may be
	_ = pointerOrInteger << 32 // reported only on a 32 bit target
	var pointerOrNarrow PointerOrNarrow
	_ = pointerOrNarrow << 15 // clean
	_ = pointerOrNarrow << 16 // reported, may be 16 bits
	var words Words
	words <<= 64 // reported, one width
}

// An intersection narrows the type set: int8 | int64 meeting int8 | int16 holds int8 alone, so the message names one
// width, not several.
func Intersected[Narrowed interface {
	int8 | int64
	int8 | int16
}, Embedded interface {
	Mixed
	~int8 | ~uint8
}, Joined interface{ Mixed | ~int16 }, Methods interface {
	~uint32 | ~uint64
	String() string
}]() {
	var narrowed Narrowed
	_ = narrowed << 8 // reported, 8 bits alone
	_ = narrowed << 7 // clean
	var embedded Embedded
	_ = embedded << 8 // reported, 8 bits alone, the intersection with Mixed
	var joined Joined
	_ = joined << 8  // reported, may be 8 bits, the union with Mixed's terms
	_ = joined << 16 // reported
	var methods Methods
	_ = methods << 32 // reported, may be 32 bits, with a method in the constraint
}

func Imported[FromThere Narrow, Exact Small | int16]() {
	var fromThere FromThere
	_ = fromThere << 8 // reported, may be 8 bits, a constraint from a dot import
	var exact Exact
	_ = exact << 16 // reported, 16 bits, a named type and its underlying type
}

// A type parameter through an alias declared in the function, and a parameter of a generic type's method.
type Container[Element ~uint8 | ~uint16] struct {
	element Element
}

func (container Container[Element]) Shift() Element {
	_ = container.element << 8 // reported, may be 8 bits
	return container.element << 7
}

func LocalAlias[Element ~uint8 | ~int16]() {
	type local = Element
	var aliased local
	_ = aliased << 8 // reported, may be 8 bits, through an alias of a type parameter
}

func Instantiated() {
	var container Container[uint16]
	_ = container.element << 16 // reported, 16 bits, an instantiated field
	_ = container.Shift()
}

// Dead code: every branch of the detector, and the shapes it takes as live.
func Dead(live bool) {
	var integer int
	const integerBits = 8 * unsafe.Sizeof(integer)
	var narrow uint8

	if false {
		_ = narrow << 8 // clean, a false condition
	}
	if true {
		_ = narrow << 7
	} else {
		_ = narrow << 8 // clean, the else of a true condition
	}
	if true {
		_ = narrow << 8 // reported, the body of a true condition
	}
	if narrow<<8 > 0 && false { // reported, the condition itself is judged
	}
	if initial := narrow << 8; false { // reported, the init statement is live
		_ = initial
	}
	if integerBits == 32 {
		_ = integer << 16
	} else if integerBits == 64 {
		_ = integer << 63 // clean, under the width, and dead on a 32 bit target
	} else {
		_ = integer << 128 // clean on both, a constant else chain
	}
	if live {
		_ = narrow << 8 // reported, a condition that is not constant
	} else {
		_ = narrow << 9 // reported
	}
	if runtime.GOARCH == "nonexistent" {
		_ = narrow << 8 // clean, a constant comparison of strings
	}
	if false {
		if true {
			_ = narrow << 8 // clean, nested in a dead body
		}
		func() {
			_ = narrow << 8 // clean, a closure in a dead body
		}()
	}

	// A switch on an integer constant.
	switch integerBits {
	case 16, 32:
		_ = integer << 64 // clean on a 64 bit target, no value matches
	case 64:
		_ = integer << 32 // clean on a 64 bit target, the matching case
	default:
		_ = narrow << 8 // reported, the default is never dead
	}
	switch skipped := 0; integerBits {
	case 1:
		_ = narrow << 8 // clean, with an init statement
		_ = skipped
	case uintptr(live2()):
		_ = narrow << 8 // reported, a case that is not constant
	}
	switch -1 {
	case 1:
		_ = narrow << 8 // reported, a negative tag is not compared
	}
	switch int64(integerBits) {
	case -1:
		_ = narrow << 8 // reported, a negative case value is not compared
	case 1:
		_ = narrow << 8 // clean, a case value other than the tag's
	}
	switch runtime.GOARCH {
	case "nonexistent":
		_ = narrow << 8 // reported, a switch on a string is taken as live
	}
	switch integer {
	case 1:
		_ = narrow << 8 // reported, a tag that is not constant
	}
	switch true {
	case false:
		_ = narrow << 8 // reported, a boolean tag is taken as live
	}

	// A tagless switch.
	switch {
	case false:
		_ = narrow << 8 // clean, the constant false
	case false, integerBits < 8:
		_ = narrow << 8 // clean, every expression the constant false
	case false, live:
		_ = narrow << 8 // reported, one expression is not constant
	case integerBits > 0:
		_ = narrow << 8 // reported, the constant true
	default:
		_ = narrow << 8 // reported, the default is never dead
	}
	switch initial := live; {
	case false:
		_ = narrow << 8 // clean, with an init statement
	case initial:
		_ = narrow << 9 // reported
	}

	// Shapes the detector takes as live, as the original does.
	for false {
		_ = narrow << 8 // reported, a loop's condition is not read
	}
	var anything any = narrow
	switch anything.(type) {
	case uint8:
		_ = narrow << 8 // reported, a type switch is not read
	}
	const never = false
	if never {
		_ = narrow << 8 // clean, a named false constant
	}
	if !never {
		_ = narrow << 7
	} else {
		narrow <<= 8 // clean, a dead shift assignment
	}
}

func live2() int { return 1 }
