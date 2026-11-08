// Package parity holds comparisons of integers of every kind with their types' limits, by the math package's names
// and as literals, from both sides and under every operator, and comparisons the check leaves alone, where the port
// and staticcheck's SA4003 still agree finding for finding. The differential test runs both over each and compares.
package parity

import (
	"math"
	limits "math"
)

type small uint8

type signed int16

type pointer uintptr

const MaxUint8 = 255

func unsignedZero(u8 uint8, u32 uint32, u uint, p uintptr, named small) {
	// Found by both, one each: an unsigned value is never below zero, and always at least zero.
	_ = u8 < 0
	_ = 0 > u32
	_ = u >= 0
	_ = 0 <= p
	_ = named < 0
	// Found by both: zero in parentheses and under signs is still a literal zero.
	_ = u8 < (0)
	_ = u8 < -0
	_ = u8 >= +(0)
	// Found by neither: other comparisons with zero.
	_ = u8 > 0
	_ = u8 <= 0
	_ = 0 < u8
	_ = 0 >= u8
	_ = u8 == 0
	_ = u8 != 0
}

func unsignedGreatest(u8 uint8, u16 uint16, u32 uint32, u64 uint64, u uint, named small) {
	// Found by both, one each: past or at the greatest value, by name.
	_ = u8 > math.MaxUint8
	_ = u8 >= math.MaxUint8
	_ = u8 <= math.MaxUint8
	_ = math.MaxUint8 < u8
	_ = math.MaxUint8 >= u8
	_ = u16 > math.MaxUint16
	_ = u32 >= math.MaxUint32
	_ = u64 > math.MaxUint64
	_ = u > math.MaxUint64
	_ = named > math.MaxUint8
	// Found by both: the same as literals, in decimal, hexadecimal and with underscores.
	_ = u8 > 255
	_ = u8 <= 0xff
	_ = u16 >= 65_535
	_ = u64 > 18446744073709551615
	// Found by both: the math package under another name.
	_ = u8 > limits.MaxUint8
	// Found by neither: another type's limit, a local constant of the same name, a conversion, and a constant
	// expression.
	_ = u16 > math.MaxUint8
	_ = u8 > MaxUint8
	_ = u8 > uint8(255)
	_ = u8 > 254+1
	_ = u > math.MaxUint32
}

func signedLimits(i8 int8, i16 int16, i32 int32, i64 int64, i int, r rune, named signed) {
	// Found by both, one each: the least and greatest values, by name and as literals, from both sides.
	_ = i8 > math.MaxInt8
	_ = i8 < math.MinInt8
	_ = i8 >= math.MinInt8
	_ = i8 <= math.MinInt8
	_ = math.MinInt8 > i8
	_ = math.MinInt8 <= i8
	_ = i16 >= math.MaxInt16
	_ = i32 > math.MaxInt32
	_ = r < math.MinInt32
	_ = i64 > math.MaxInt64
	_ = i < math.MinInt64
	_ = named > math.MaxInt16
	_ = i8 >= -128
	_ = i8 > 127
	_ = i8 < -(128)
	_ = i8 <= -+128
	// Found by neither: zero and values inside the range.
	_ = i8 < 0
	_ = i8 >= 0
	_ = i8 > 126
	_ = i8 < -127
	_ = i16 > math.MaxInt8
	_ = i > math.MaxInt32
}

func others(value float64, text string, p pointer) {
	// Found by neither: a float compared with an integer limit.
	_ = value > math.MaxInt8
	_ = value < math.MinInt64
	// Found by neither: strings.
	_ = text > "z"
	// Found by both: an unsigned pointer-sized type below zero.
	_ = p < 0
	// Found by neither: its greatest value, which the check does not know.
	_ = p > math.MaxUint64
}

// Found by both: type parameters with one core type.
func genericCore[T ~uint8, U ~int8](unsigned T, signed U) {
	_ = unsigned > math.MaxUint8
	_ = unsigned < 0
	_ = signed < math.MinInt8
}

// Found by both, once: with more than one core type no limit is judged, and of the two only the unsigned one is
// judged below zero, since the other's types include a signed one.
func genericMixed[T uint8 | uint16, U int8 | uint8](unsigned T, mixed U) {
	_ = unsigned < 0
	_ = unsigned > 255
	_ = mixed < 0
	_ = mixed > 127
}
