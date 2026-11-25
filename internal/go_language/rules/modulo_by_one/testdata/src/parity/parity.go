// Package parity holds the remainders SA4028 judges, flagged and left alone, for the port and the original to judge
// side by side: the divisor 1 in every spelling an integer literal takes, under signs and parentheses, and the
// divisors that are 1 only by a constant, a conversion or arithmetic, which the pattern refuses.
package parity

type count int

const one = 1

const typedOne count = 1

// remainderOfConstants is a constant expression, and the pattern matches it all the same.
const remainderOfConstants = 7 % 1

var arrayOfNone [5 % 1]int

func flagged(value int, unsigned uint, small int8, large uint64, counted count) {
	_ = value % 1
	_ = unsigned % 1
	_ = small % 1
	_ = large % 1
	_ = counted % 1
	_ = value % 0x1
	_ = value % 01
	_ = value % 0o1
	_ = value % 0b1
	_ = value % 0_1
	_ = value % 0x_1
	_ = value % +1
	_ = value % (1)
	_ = value % (+1)
	_ = value % - -1
	_ = value % -(-1)
	_ = value % -(+(-(1)))
	_ = (value % 1)
	_ = (value) % 1
	_ = -value % 1
	_ = value*2%1 + 3
	_ = value % 1 % 1
	_ = (value % 1) % 1
	_ = value%1 == 0
	_ = -9 % 1
	_ = func() int { return value % 1 }
	if value%1 != 0 {
		return
	}
}

func notFlagged(value int, unsigned uint, ratio float64) {
	_ = value % 2
	_ = value % -1
	_ = value % 1_0
	_ = value % 10
	_ = value % one
	_ = value % (one)
	_ = count(value) % typedOne
	_ = value % int(1)
	_ = value % (int)(1)
	_ = unsigned % uint(1)
	_ = value % (2 - 1)
	_ = value % (1 + 0)
	_ = value % (1 << 0)
	_ = value % ^-2
	_ = value % '\x01'
	_ = value % 1.0
	_ = value % 1e0
	_ = 1 % value
	_ = value / 1
	_ = value & 1
	_ = value * 1
	_ = ratio / 1
	value %= 1
	unsigned %= 1
}

func generic[Integer ~int | ~uint8](value Integer) Integer {
	_ = value % 1
	_ = value % (1)
	_ = value % Integer(1)
	return value % 1
}

func instantiate() {
	_ = generic[int](3)
	_ = generic(uint8(3))
}
