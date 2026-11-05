package lookalike

type celsius float64

type node struct{ name string }

func use(value any) {}

// A conversion reads like an assertion, Type(value), and is checked when it compiles.
func conversions(count int, pointer *node) {
	use(int64(count))
	use(celsius(count))
	use((*node)(pointer))
	use(any(count))
}

// Map index, channel receive and a call returning two values all take the comma-ok shape and assert nothing.
func commaOkReads(values map[string]int, numbers chan int) {
	value, found := values["key"]
	number, open := <-numbers
	typed, ok := convert(value)
	use(found)
	use(number)
	use(open)
	use(typed)
	use(ok)
}

func convert(value any) (*node, bool) { return nil, false }

// A function named for an assertion is an ordinary call.
func assert(value any) *node { return nil }

func named(value any) {
	use(assert(value))
}

// A generic function instantiated with a type argument has the same brackets-and-parentheses look.
func identity[Element any](value Element) Element { return value }

func instantiated(count int) {
	use(identity[int](count))
}
