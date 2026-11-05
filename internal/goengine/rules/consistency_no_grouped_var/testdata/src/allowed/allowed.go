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

const (
	packageLow  = 1
	packageHigh = 2
)

var (
	packageLeft, packageRight int
)

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

// iota counts the specs of one block, so a function-level const block that counts with it needs the block.
func groupedConstWithIota() {
	const (
		first = iota
		second
	)
	use(first, second)
}

// iota inside a larger value counts the same way.
func groupedConstWithShiftedIota() {
	const (
		read = 1 << iota
		write
		execute
	)
	use(read, write, execute)
}

// A block whose first value is fixed and whose later values count with iota still needs the block for its count.
func groupedConstWithLaterIota() {
	const (
		unknown = -1
		first   = iota
		second
	)
	use(unknown, first, second)
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
