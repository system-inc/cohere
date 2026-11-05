package flagged

func use(values ...any) {}

func read() (int, error) { return 0, nil }

func count() int { return 0 }

func groupedVar() {
	var ( // want "groups declarations inside a function"
		width  = 3
		height = 4
	)
	use(width, height)
}

// A grouped block with one spec is still a group, and its parentheses still hide the statement's shape.
func groupedVarOfOne() {
	var ( // want "groups declarations inside a function"
		width = 3
	)
	use(width)
}

// iota makes an enum at package level; inside a function the block is judged like any other.
func groupedConstWithIota() {
	const ( // want "groups declarations inside a function"
		first = iota
		second
	)
	use(first, second)
}

func severalNamesWithType() {
	var first, second int // want "declares .first, second. in one statement"
	use(first, second)
}

func severalNamesWithValues() {
	var first, second = 1, 2 // want "declares .first, second. in one statement"
	use(first, second)
}

func severalConstNames() {
	const low, high = 1, 2 // want "declares .low, high. in one statement"
	use(low, high)
}

// Two calls are two values, so each name has a value of its own to sit beside.
func severalNamesFromTwoCalls() {
	var left, right = count(), count() // want "declares .left, right. in one statement"
	use(left, right)
}

func blankAmongNames() {
	var _, second = 1, 2 // want "declares ._, second. in one statement"
	use(second)
}

func threeNames() {
	var red, green, blue byte // want "declares .red, green, blue. in one statement"
	use(red, green, blue)
}

// The block and the list of names inside it are each their own fix, so both are reported.
func groupedWithSeveralNames() {
	var ( // want "groups declarations inside a function"
		first, second    int // want "declares .first, second. in one statement"
		value, readError = read()
	)
	use(first, second, value, readError)
}

func multiByteNames() {
	var größe, breite int // want "declares .größe, breite. in one statement"
	use(größe, breite)
}

type counter struct{}

func (counter counter) method() {
	var first, second string // want "declares .first, second. in one statement"
	use(counter, first, second)
}

func closure() {
	run := func() {
		var first, second int // want "declares .first, second. in one statement"
		use(first, second)
	}
	run()
}

func generic[Element any]() {
	var first, second Element // want "declares .first, second. in one statement"
	use(first, second)
}

// A literal assigned at package level is still a function body.
var handler = func() {
	var first, second int // want "declares .first, second. in one statement"
	use(first, second)
}
