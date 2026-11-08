// Package parity holds every function the original judges given values converted from each kind of number: integers
// signed and not, named and not, byte, rune and uintptr, floats, type parameters whose type sets hold integers alone
// and more, constants, values not converted, and the shapes staticcheck's representation builds the call in more
// than one way.
package parity

import "math"

type count int

type measure float32

type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

const seven = 7

var global = 3

var rounded = math.Floor(float64(global))

func init() {
	_ = math.Ceil(float64(global))
}

func everyFunction(number int) {
	math.Ceil(float64(number))
	math.Floor(float64(number))
	math.Trunc(float64(number))
	math.IsNaN(float64(number))
	math.IsInf(float64(number), 0)
	math.IsInf(float64(number), 1)
	math.Round(float64(number))
	math.Abs(float64(number))
	math.Sqrt(float64(number))
}

func integers(small int8, unsigned uint64, letter byte, character rune, address uintptr, counted count) {
	math.Ceil(float64(small))
	math.Ceil(float64(unsigned))
	math.Ceil(float64(letter))
	math.Ceil(float64(character))
	math.Ceil(float64(address))
	math.Ceil(float64(counted))
	math.Ceil(float64(small + 1))
	math.Ceil(float64(len("text")))
	math.Ceil(float64(counted) / 2)
	math.Ceil(float64(small) + 0.5)
}

func notIntegers(real float64, single float32, measured measure, flag bool) {
	math.Ceil(real)
	math.Ceil(float64(real))
	math.Ceil(float64(single))
	math.Ceil(float64(measured))
	math.Ceil(1.5)
	math.Ceil(7)
	math.Ceil(float64(seven))
	math.Ceil(float64(3))
	math.IsNaN(math.NaN())
	_ = flag
}

func phis(condition bool, number int, real float64) {
	value := float64(number)
	if condition {
		value = real
	}
	math.Floor(value)
	agreeing := float64(number)
	if condition {
		agreeing = float64(number)
	}
	math.Floor(agreeing)
	kept := float64(number)
	math.Floor(kept)
}

func callShapes(number int) {
	defer math.Ceil(float64(number))
	go math.Floor(float64(number))
	func() {
		math.Trunc(float64(number))
	}()
	ceil := math.Ceil
	ceil(float64(number))
	math.Ceil(
		float64(number),
	)
	if math.IsNaN(float64(number)) || math.IsInf(float64(number), -1) {
		return
	}
	_ = math.Ceil(math.Floor(float64(number)))
	_ = rounded
}

func generic[Integer integer, Mixed ~int | ~float64, Any any, Float ~float32 | ~float64](
	whole Integer, mixed Mixed, anything Any, real Float,
) {
	math.Ceil(float64(whole))
	math.Ceil(float64(mixed))
	math.Ceil(float64(real))
	_ = anything
}

func single[Integer ~int](whole Integer) {
	math.Floor(float64(whole))
}

func instantiated() {
	generic[int, float64, string, float32](1, 2, "", 3)
	single(count(1))
}
