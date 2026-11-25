// Package parity holds the negative zeros SA4026 judges, flagged and left alone, for the port and the original to
// judge side by side: -0.0, -float32(0) and float64(-0) with parentheses around every part, the literals the pattern
// refuses for their text, conversions it refuses for their name, and float32 and float64 the code declares as
// variables, which the original reports as -0.0.
package parity

type myFloat float64

const zero = 0.0

func flagged() {
	_ = -0.0
	_ = -(0.0)
	_ = (-0.0)
	_ = float32(-0.0)
	_ = float64(-(0.0))
	_ = -float32(0)
	_ = -float64(0)
	_ = -float32(0.0)
	_ = -float64(0.0)
	_ = -(float64)(0)
	_ = -float64((0))
	_ = -(float32(0.0))
	_ = (-float64(0))
	_ = float64(-0)
	_ = float32(-0)
	_ = float64(-(0))
	_ = float64((-0))
	_ = (float32)(-0)
	_ = -(-0.0)
	_ = []float64{-0.0, -float64(0), float64(-0)}
	_ = func() float64 { return -0.0 }
}

func notFlagged(value float64) {
	_ = -0.
	_ = -0.00
	_ = -.0
	_ = -0e0
	_ = -0
	_ = -00
	_ = -0x0
	_ = +0.0
	_ = 0.0
	_ = -zero
	_ = -value
	_ = -float64(0.)
	_ = -float64(00)
	_ = -float64(0x0)
	_ = -float64(1)
	_ = -float64(zero)
	_ = -int(0)
	_ = -myFloat(0)
	_ = float64(-00)
	_ = float64(-1)
	_ = float64(+0)
	_ = int(-0)
	_ = myFloat(-0)
	_ = complex128(-0)
}

func declaredVariables() {
	float64 := func(value float32) float32 { return value }
	_ = -float64(0.0)
	float32 := func(value int) int { return value }
	_ = -float32(0)
	_ = float32(-0)
}

func generic[Float ~float32 | ~float64]() Float {
	_ = -Float(0)
	_ = Float(-0)
	return -0.0
}

func instantiate() {
	_ = generic[float32]()
	_ = generic[myFloat]()
}
