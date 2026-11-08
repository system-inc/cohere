// Package parity holds the divisions SA4025 judges, flagged and left alone, for the port and the original to judge
// side by side: two integer literals in every spelling, under signs and parentheses, wherever the result lands, and
// the divisions the pattern refuses for a named constant, a float or character literal, a conversion or arithmetic on
// either side, or a result that truncates to anything but zero.
package parity

type ratio float64

const three = 3

const truncated = 2 / 3

const truncatedFloat float64 = 1 / 2

var lengthOfNone [1 / 2]int

func takesFloat(value float64) {}

func takesRatio(value ratio) {}

func flagged() float64 {
	_ = 1 / 2
	_ = 0 / 1
	_ = 0x1 / 0x2
	_ = 01 / 0o2
	_ = 0b1 / 2
	_ = 1_0 / 1_00
	_ = -1 / 2
	_ = 1 / -2
	_ = +1 / +2
	_ = - -1 / 2
	_ = (1) / 2
	_ = 1 / (2)
	_ = (1 / 2)
	_ = (-(1)) / (+(2))
	_ = 2 / 3 / 4
	_ = 1/2 + 3
	_ = 3 + 1/2
	var narrow uint8 = 1 / 2
	_ = narrow
	var wide float32 = 1 / 3
	_ = wide
	_ = float64(2 / 3)
	_ = ratio(1 / 4)
	takesFloat(1 / 2)
	takesRatio(1 / 2)
	_ = func() float64 { return 1 / 2 }
	return 1 / 2
}

func notFlagged(value int) {
	_ = 4 / 2
	_ = 4 / 3
	_ = -3 / 2
	_ = 3 / -2
	_ = -4 / 3
	_ = 2 / 1
	_ = three / 4
	_ = 1 / three
	_ = 2 / 3.
	_ = 2 / 3.0
	_ = 2.0 / 3
	_ = 1e0 / 2
	_ = '\x01' / 2
	_ = 1 / '\x02'
	_ = int(1) / 2
	_ = 1 / int(2)
	_ = (1 + 0) / 2
	_ = 1 / (1 + 1)
	_ = ^-2 / 3
	_ = (2 / 3) / 4
	_ = value / 2
	_ = 1 / value
	_ = 1 % 2
	_ = 1 * 2
	value /= 2
}

func generic[Number ~int | ~float64]() Number {
	_ = Number(1) / 2
	return 1 / 2
}

func instantiate() {
	_ = generic[int]()
	_ = generic[float64]()
}
