// Package integers holds every way an expression can be, or only look like, an integer literal as staticcheck's
// IntegerLiteral reads one, for the port and the original to judge side by side.
package integers

type count int

const one = 1

const typedOne count = 1

func literals(value int, unsigned uint, small int8) {
	// Integer literals, in every base and spelling gofmt keeps.
	_ = 1
	_ = 0x1
	_ = 01
	_ = 0o1
	_ = 0b1
	_ = 0_1
	_ = 0x_1
	_ = 1_0
	_ = 1_000_000
	_ = 0
	_ = 9223372036854775807
	_ = uint64(18446744073709551615)
	_ = -9223372036854775808

	// Under signs and parentheses.
	_ = -1
	_ = +1
	_ = - -1
	_ = -+42
	_ = +(+1)
	_ = (1)
	_ = -(1)
	_ = (-(-(1)))
	_ = -(+(-(1)))

	// Constant, but not literals.
	_ = one
	_ = -one
	_ = (one)
	_ = typedOne
	_ = count(1)
	_ = int(1)
	_ = (int)(1)
	_ = -int(1)
	_ = ^-2
	_ = 2 - 1
	_ = 1 + 0
	_ = 1 << 0
	_ = '\x01'
	_ = -'a'
	_ = 1.0
	_ = 1e0
	_ = -1.0
	_ = 1i
	_ = "1"
	_ = len("a")
	_ = len([1]int{})
	_ = !true

	// Not constant at all.
	_ = value
	_ = -value
	_ = +value
	_ = ^value
	_ = value % 1
	_ = unsigned % 1
	_ = small % -1
	value %= 1
}

func typedContexts() (int8, uint64, float64, complex128) {
	return -1, 1, 1, 1
}

func generic[Integer ~int | ~uint](value Integer) Integer {
	_ = value % 1
	_ = value % (1)
	_ = value % Integer(1)
	return +1
}

func instantiate() {
	_ = generic[int](1)
	_ = generic(uint(1))
}
