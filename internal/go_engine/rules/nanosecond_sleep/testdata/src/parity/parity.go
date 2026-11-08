// Package parity holds every way the query accepts or refuses a call: the callee through parentheses, a method value,
// and functions and methods named Sleep that are not time's, and the argument as an integer literal in every base,
// under unary signs and parentheses, at and around the bounds, and as a named constant, a conversion, a character, a
// float and arithmetic.
package parity

import "time"

const five = 5

type sleeper struct{}

func (sleeper) Sleep(duration time.Duration) {}

func Sleep(duration time.Duration) {}

var slept = func() bool {
	time.Sleep(3)
	return true
}()

func init() {
	time.Sleep(4)
}

func bounds() {
	time.Sleep(0)
	time.Sleep(1)
	time.Sleep(2)
	time.Sleep(119)
	time.Sleep(120)
	time.Sleep(121)
	time.Sleep(1000)
	time.Sleep(-1)
	time.Sleep(-120)
	time.Sleep(-121)
	time.Sleep(-1000000)
	time.Sleep(-0)
	time.Sleep(9223372036854775807)
	time.Sleep(-9223372036854775808)
}

func spellings() {
	time.Sleep(0x10)
	time.Sleep(0x1F)
	time.Sleep(0o17)
	time.Sleep(017)
	time.Sleep(0b101)
	time.Sleep(1_0)
	time.Sleep(+7)
	time.Sleep(-+7)
	time.Sleep(+-7)
	time.Sleep(-(8))
	time.Sleep(-(+(-(9))))
	time.Sleep((10))
}

func notLiterals(variable time.Duration) {
	time.Sleep(five)
	time.Sleep(time.Duration(5))
	time.Sleep(time.Duration(5) * time.Nanosecond)
	time.Sleep(2 + 3)
	time.Sleep(^-5)
	time.Sleep('a')
	time.Sleep(1.0)
	time.Sleep(1e2)
	time.Sleep(variable)
	time.Sleep(5 * time.Second)
	time.Sleep(time.Nanosecond)
	time.Sleep(-five)
}

func callees() {
	(time.Sleep)(11)
	(time.Sleep)(12)
	sleep := time.Sleep
	sleep(13)
	Sleep(14)
	sleeper{}.Sleep(15)
	var value sleeper
	value.Sleep(16)
	defer time.Sleep(17)
	go time.Sleep(18)
	func() {
		time.Sleep(19)
	}()
	time.AfterFunc(20, func() {})
	_ = slept
}

func generic[T any](value T) T {
	time.Sleep(21)
	return value
}

func instantiated() {
	generic(1)
}
