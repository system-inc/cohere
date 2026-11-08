// Package parity holds the comparisons SA4024 judges, flagged and left alone, for the port and the original to judge
// side by side: len and cap of every kind of operand compared below zero, in both of the orders the pattern names,
// zero in every spelling an integer literal takes, parentheses anywhere the pattern looks through them, and the
// comparisons it refuses for their order, their operator, a zero that is no literal or a len that is no builtin.
package parity

type list []int

type thing struct{}

func (thing) len() int { return 0 }

const zero = 0

func flagged(values []int, text string, table map[string]int, channel chan int, array [3]int, named list) bool {
	_ = len(values) < 0
	_ = cap(values) < 0
	_ = 0 > len(values)
	_ = 0 > cap(values)
	_ = len(text) < 0
	_ = len(table) < 0
	_ = len(channel) < 0
	_ = cap(channel) < 0
	_ = len(array) < 0
	_ = cap(array) < 0
	_ = len(named) < 0
	_ = len("abc") < 0
	_ = len(values) < 0x0
	_ = len(values) < 00
	_ = len(values) < 0o0
	_ = len(values) < 0b0
	_ = len(values) < 0_0
	_ = len(values) < -0
	_ = len(values) < +0
	_ = len(values) < - -0
	_ = len(values) < (0)
	_ = (len(values)) < 0
	_ = (len)(values) < 0
	_ = 0 > (cap(values))
	_ = (0) > (cap)(values)
	_ = (len(values) < 0)
	_ = len(values) < 0 == true
	_ = len(values) < 0 || 0 > cap(values)
	_ = len(values[1:]) < 0
	_ = func() bool { return len(text) < 0 }
	if len(values) < 0 {
		return true
	}
	for 0 > cap(values) {
	}
	switch {
	case len(table) < 0:
	}
	println(len(values) < 0)
	return 0 > len(values)
}

func notFlagged(values []int, other []int, number int, item thing) {
	_ = len(values) <= 0
	_ = len(values) > 0
	_ = len(values) < 1
	_ = len(values) == 0
	_ = len(values) != 0
	_ = 0 < len(values)
	_ = 0 >= len(values)
	_ = 0 > -len(values)
	_ = len(values) > 0
	_ = len(values) < zero
	_ = zero > len(values)
	_ = len(values) < int(0)
	_ = len(values) < 0.0
	_ = 0.0 > len(values)
	_ = len(values) < '\x00'
	_ = len(values) < 0-0
	_ = len(values) < ^-1
	_ = copy(values, other) < 0
	_ = item.len() < 0
	_ = number < 0
	_ = len(values)+1 < 0
	{
		len := func(values []int) int { return 1 }
		cap := len
		_ = len(values) < 0
		_ = 0 > cap(values)
	}
}

func generic[Collection ~[]int | ~string](collection Collection) bool {
	_ = len(collection) < 0
	return 0 > len(collection)
}

func genericCapacity[Collection ~[]int](collection Collection) bool {
	return cap(collection) < 0
}

func instantiate() {
	_ = generic([]int{1})
	_ = generic("one")
	_ = genericCapacity(list{})
}
