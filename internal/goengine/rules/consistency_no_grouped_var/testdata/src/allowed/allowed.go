package allowed

func use(values ...any) {}

func read() (int, error) { return 0, nil }

// A grouped const with iota at package level is how Go writes an enum.
const (
	Sunlight = iota
	Moonlight
	Starlight
)

// Package-level declarations are not judged, grouped or listed.
var (
	defaultWidth  = 3
	defaultHeight = 4
)

var packageFirst, packageSecond int

func oneNamePerStatement() {
	var width = 3
	var height int
	const limit = 10
	use(width, height, limit)
}

// A multi-return call yields every name, so they cannot be split across declarations.
func multiReturnCall() {
	var value, readError = read()
	use(value, readError)
}

// A comma-ok read yields its two names the same way a multi-return call does.
func commaOk(cache map[string]int, anything any, channel chan int) {
	var cached, found = cache["key"]
	var text, isString = anything.(string)
	var received, open = <-channel
	use(cached, found, text, isString, received, open)
}

// A short declaration is never judged: a multi-return call is grammar, and a parallel assignment is how Go swaps.
func shortDeclarations() {
	value, readError := read()
	first, second := 1, 2
	first, second = second, first
	use(value, readError, first, second)
}

// A grouped type block declares types, not values, and is no var or const.
func groupedTypes() {
	type (
		point struct{ x, y int }
		size  struct{ width, height int }
	)
	use(point{}, size{})
}
