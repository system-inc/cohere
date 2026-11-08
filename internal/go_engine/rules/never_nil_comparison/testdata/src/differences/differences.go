// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none. The original finds the function an if statement belongs to with
// ir.EnclosingFunction, by its declaration's position and then each function literal's down the path to it, where
// its translation walks each function's own syntax and leaves the literals inside to the functions they are; these
// are if statements nested in literals inside literals, in initializers, in conditions and in methods of generic
// types, where a mismatch would judge a statement in the wrong function, or in none, and show here as a finding one
// side makes and the other does not.
package differences

type box[T any] struct {
	items []T
}

func use(values ...any) {}

// Found by both: one in each of three nested function literals, each judged in its own.
func nested() {
	outer := new(int)
	func() {
		middle := new(int)
		func() {
			inner := new(int)
			if inner != nil {
			}
		}()
		if middle != nil {
		}
	}()
	if outer != nil {
	}
}

// Found by both: in a function literal inside a method of a generic type, and in the method.
func (receiver box[T]) method() {
	items := make([]T, 0)
	func() {
		table := make(map[int]T)
		if table != nil {
		}
	}()
	if items != nil {
	}
}

// Found by both: function literals inside an if statement's condition belong to the literal, and their own if
// statements to themselves.
func literalInCondition() {
	if check := func() bool {
		pointer := new(int)
		if pointer != nil {
		}
		return true
	}; check != nil {
	}
}

// Found by both: closures in a package-level initializer, one inside another.
var initializer = func() func() {
	first := new(int)
	if first != nil {
	}
	return func() {
		second := make(chan int)
		if second != nil {
		}
	}
}

// Found by both: one variable with many values, every edge of its phis related, in position order.
func manyValues(condition bool, count int) {
	values := make([]int, 0)
	switch {
	case condition:
		values = make([]int, 1)
	case count > 2:
		values = []int{1}
	case count > 1:
		var array [3]int
		values = array[:]
	}
	if values == nil {
	}
	use(values)
}
