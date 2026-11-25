// Package parity holds every comparison shape `ST1017` reads, each found and each near miss, for the original and the
// port to judge alike.
package parity

import "time"

// limit is a named constant, which is written with an identifier, so it is never truly constant.
const limit = 3

// A comparison at package scope is judged as one in a function is.
var packageLevel = 0 == limit

func comparisons(text string, number int, ratio float64, complexNumber complex128, letter rune, pointer *int,
	flag bool, duration time.Duration) {
	// Each kind of literal first, with == and !=.
	_ = "" == text
	_ = 0 != number
	_ = 0.5 == ratio
	_ = 1i == complexNumber
	_ = 'a' == letter
	_ = -1 == number
	_ = 1<<3 == number
	_ = "a"+"b" != text
	_ = -(2 + 3) == number

	// Parentheses around either operand, which the fix leaves out.
	_ = (0) == number
	_ = 0 == (number)
	_ = (-1) == (number)
	_ = (0 == number)

	// A constant that names an identifier on the right is not truly constant, so the comparison is reported.
	_ = 0 == limit
	_ = "" == "" || 0 == number
	_ = 0 == len(text)

	// Nested comparisons, each judged on its own.
	_ = (0 == number) == flag
	_ = flag == (0 == number)
	_ = (1 == 1) == flag

	// Spread over lines, which the printed fix follows.
	_ = 0 ==
		number
	_ = 0 == number+
		1

	// Near misses: the constant on the right, both sides constant, another operator, and a left side written with an
	// identifier.
	_ = number == 0
	_ = 0 == 0
	_ = "" == ""
	_ = 0 < number
	_ = 0 >= number
	_ = 0 + number
	_ = limit == number
	_ = nil == pointer
	_ = true == flag
	_ = int64(0) == int64(number)
	_ = len("abc") == number
	_ = time.Second == duration
	_ = 0 == 0.0
	if 0 == number {
	}
	switch {
	case 0 == number:
	}
}
