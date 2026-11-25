// Package parity holds every function the original judges, called with each bit size, base and format at and around
// the bounds it accepts, constants written in each way and values that are not constant, and the shapes
// staticcheck's representation builds the call in more than one way.
package parity

import "strconv"

type size int

const (
	floatSize       = 32
	badSize         = 16
	sizeOfType size = 128
	format          = 'q'
	byteFormat byte = 'g'
)

var parsed, _ = strconv.ParseFloat("1", 8)

func init() {
	_ = strconv.FormatInt(1, 40)
}

func bitSizes(text string) {
	strconv.ParseComplex(text, 0)
	strconv.ParseComplex(text, 63)
	strconv.ParseComplex(text, 64)
	strconv.ParseComplex(text, 128)
	strconv.ParseComplex(text, 129)
	strconv.ParseFloat(text, 0)
	strconv.ParseFloat(text, 31)
	strconv.ParseFloat(text, 32)
	strconv.ParseFloat(text, 64)
	strconv.ParseFloat(text, 65)
	strconv.ParseFloat(text, -32)
	strconv.ParseInt(text, 10, -1)
	strconv.ParseInt(text, 10, 8)
	strconv.ParseInt(text, 10, 64)
	strconv.ParseInt(text, 10, 65)
	strconv.ParseUint(text, 10, -64)
	strconv.ParseUint(text, 10, 32)
	strconv.ParseUint(text, 10, 1024)
	strconv.FormatComplex(1, 'g', -1, 32)
	strconv.FormatComplex(1, 'g', -1, 64)
	strconv.FormatComplex(1, 'g', -1, 128)
	strconv.FormatFloat(1, 'g', -1, 16)
	strconv.FormatFloat(1, 'g', -1, 32)
	strconv.AppendFloat(nil, 1, 'g', -1, 0)
	strconv.AppendFloat(nil, 1, 'g', -1, 64)
}

func bases(text string, number int64, unsigned uint64) {
	strconv.ParseInt(text, 0, 64)
	strconv.ParseInt(text, 1, 64)
	strconv.ParseInt(text, -2, 64)
	strconv.ParseInt(text, 2, 64)
	strconv.ParseInt(text, 36, 64)
	strconv.ParseInt(text, 100, 64)
	strconv.ParseUint(text, 0, 64)
	strconv.ParseUint(text, 1, 64)
	strconv.ParseUint(text, 37, 64)
	strconv.FormatInt(number, 0)
	strconv.FormatInt(number, -5)
	strconv.FormatInt(number, 16)
	strconv.FormatInt(number, 64)
	strconv.FormatUint(unsigned, 1)
	strconv.FormatUint(unsigned, 8)
	strconv.FormatUint(unsigned, 99)
	strconv.AppendInt(nil, number, 0)
	strconv.AppendInt(nil, number, 10)
	strconv.AppendInt(nil, number, 37)
	strconv.AppendUint(nil, unsigned, -1)
	strconv.AppendUint(nil, unsigned, 2)
	strconv.AppendUint(nil, unsigned, 360)
	strconv.ParseInt(text, -1, -1)
	strconv.ParseUint(text, 40, 70)
}

func formats(value float64, complexValue complex128) {
	for _, valid := range []byte{'b', 'e', 'E', 'f', 'g', 'G', 'x', 'X'} {
		strconv.FormatFloat(value, valid, -1, 64)
	}
	strconv.FormatFloat(value, 'b', -1, 64)
	strconv.FormatFloat(value, 'e', -1, 64)
	strconv.FormatFloat(value, 'E', -1, 64)
	strconv.FormatFloat(value, 'f', -1, 64)
	strconv.FormatFloat(value, 'G', -1, 64)
	strconv.FormatFloat(value, 'x', -1, 64)
	strconv.FormatFloat(value, 'X', -1, 64)
	strconv.FormatFloat(value, 'd', -1, 64)
	strconv.FormatFloat(value, 'F', -1, 64)
	strconv.FormatFloat(value, 0, -1, 64)
	strconv.FormatFloat(value, '\n', -1, 64)
	strconv.FormatFloat(value, '\'', -1, 64)
	strconv.FormatFloat(value, 255, -1, 64)
	strconv.FormatFloat(value, 'é', -1, 64)
	strconv.FormatFloat(value, 'z', -1, 16)
	strconv.FormatComplex(complexValue, 'v', -1, 128)
	strconv.FormatComplex(complexValue, 'x', -1, 256)
	strconv.AppendFloat(nil, value, 's', -1, 32)
	strconv.AppendFloat(nil, value, 'f', 2, 32)
}

func constantShapes(text string, value float64) {
	strconv.ParseFloat(text, floatSize)
	strconv.ParseFloat(text, badSize)
	strconv.ParseFloat(text, int(sizeOfType))
	strconv.ParseFloat(text, 8*8)
	strconv.ParseFloat(text, 8*2)
	strconv.ParseFloat(text, 32.0)
	strconv.ParseFloat(text, 1e2)
	strconv.ParseFloat(text, (16))
	strconv.FormatFloat(value, format, -1, 64)
	strconv.FormatFloat(value, byteFormat, -1, 64)
	strconv.FormatFloat(value, "jk"[0], -1, 64)
	local := 16
	strconv.ParseFloat(text, local)
	const declared = 48
	strconv.ParseInt(text, declared, declared)
	strconv.ParseInt(text, 0x10, 0b1000)
}

func valuesNotConstant(text string, bits int, base int, verb byte, condition bool) {
	strconv.ParseFloat(text, bits)
	strconv.ParseInt(text, base, bits)
	strconv.FormatFloat(1, verb, -1, bits)
	strconv.FormatInt(1, base+40)
	differing := 16
	if condition {
		differing = 8 // Two different constants meet in a phi, which is no one constant.
	}
	strconv.ParseFloat(text, differing)
	agreeing := 16
	if condition {
		agreeing = 16
	}
	strconv.ParseFloat(text, agreeing)
}

func callShapes(text string, number int64) {
	defer strconv.ParseFloat(text, 16)
	go strconv.FormatInt(number, 1)
	func() {
		strconv.ParseInt(text, 1, 65)
	}()
	format := strconv.FormatInt
	format(number, 50)
	strconv.FormatFloat(
		1,
		'j',
		-1,
		128,
	)
	if _, parseError := strconv.ParseUint(text, 37, 65); parseError != nil {
		return
	}
	_ = strconv.FormatInt(int64(len(strconv.FormatInt(number, 1))), 99)
	_ = parsed
}

func generic[T any](value T, text string) T {
	strconv.ParseFloat(text, 16)
	return value
}

func instantiated() {
	generic(1, "")
}
