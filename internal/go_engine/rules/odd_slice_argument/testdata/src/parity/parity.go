// Package parity holds the length checks SA5012 marks and the calls it judges, flagged and left alone, for the port
// and the original to judge side by side, the marks compared as facts: every shape of the check, checks that do not
// mark, a capacity's among them, chains of passing a slice on, methods, a function of another package, and every way
// a slice's length is known or is not.
package parity

import "parity/pairs"

type logger struct{}

func notEqualZero(values ...int) {
	if len(values)%2 != 0 {
		panic("odd")
	}
}

func equalOne(values []int) {
	if len(values)%2 == 1 {
		panic("odd")
	}
}

func reversed(values ...int) {
	if 0 != len(values)%2 {
		panic("odd")
	}
}

func reversedOne(values ...int) {
	if 1 == len(values)%2 {
		panic("odd")
	}
}

func copied(values ...int) {
	same := values
	if len(same)%2 != 0 {
		panic("odd")
	}
}

func conditional(flag bool, values ...int) {
	if flag {
		if len(values)%2 != 0 {
			panic("odd")
		}
	}
}

func returning(values ...int) {
	if len(values)%2 != 0 {
		return
	}
}

func byThree(values ...int) {
	if len(values)%3 != 0 {
		panic("not in threes")
	}
}

func notALength(count int, values ...int) {
	if count%2 != 0 {
		panic("odd")
	}
}

func aCapacity(values ...int) {
	if cap(values)%2 != 0 {
		panic("odd")
	}
}

func (logger) pairs(message string, values ...any) {
	if len(values)%2 != 0 {
		panic("odd")
	}
}

func forward(values ...int) {
	notEqualZero(values...)
}

func forwardTwice(values ...int) {
	forward(values...)
}

func forwardSometimes(flag bool, values ...int) {
	if flag {
		notEqualZero(values...)
	}
}

func forwardMethod(values ...any) {
	logger{}.pairs("", values...)
}

func calls(values []int, words []string, array [5]int, item logger) {
	notEqualZero(1, 2, 3)
	notEqualZero(1, 2)
	notEqualZero()
	equalOne([]int{1})
	equalOne([]int{1, 2})
	equalOne(make([]int, 5))
	equalOne(make([]int, 5, 10))
	reversed(values[:1]...)
	reversed(values[1:4]...)
	reversed(values[2:4]...)
	reversedOne(array[:]...)
	reversedOne(array[1:]...)
	reversedOne(array[:3][1:]...)
	copied(1, 2, 3)
	conditional(true, 1, 2, 3)
	returning(1, 2, 3)
	byThree(1, 2)
	notALength(1, 1, 2, 3)
	aCapacity(1, 2, 3)
	forward(1, 2, 3)
	forwardTwice(1, 2, 3)
	forwardSometimes(true, 1, 2, 3)
	forwardMethod(1, 2, 3)
	item.pairs("", 1, 2, 3)
	logger.pairs(item, "", 1, 2, 3)
	pairs.Pairs("one")
	pairs.Forward("one", "two", "three")
	pairs.Pairs(words...)
	pairs.Pairs(words[:1]...)
	notEqualZero(values...)
	notEqualZero(nil...)
	function := notEqualZero
	function(1, 2, 3)
	func() {
		notEqualZero(1, 2, 3)
	}()
}
