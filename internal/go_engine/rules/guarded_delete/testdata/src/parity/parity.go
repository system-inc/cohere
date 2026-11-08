package parity

type holder struct {
	values map[string]int
	key    string
}

type point struct {
	x int
	y int
}

func compute() string { return "" }

func spread(values ...any) int { return len(values) }

func run(function func()) int {
	function()
	return 0
}

func reported(values map[string]int, key string, other holder, keys []string, pointer *string) {
	if _, ok := values[key]; ok {
		delete(values, key)
	}
	if _, ok := values["literal"]; ok {
		delete(values, "literal")
	}
	if _, found := values[key]; found {
		delete(values, key)
	}

	// The pattern looks through parentheses around the lookup, the map, the key, delete, its arguments and the
	// call itself; gofmt drops them around the condition, which the unformatted fixture holds.
	if _, ok := (values[key]); ok {
		(delete)((values), (key))
	}
	if _, ok := (values)[(key)]; ok {
		(delete(values, key))
	}

	// A block of one statement is looked through, as deep as it nests, and the fix keeps the call alone.
	if _, ok := values[key]; ok {
		{
			delete(values, key)
		}
	}
	if _, ok := values[key]; ok {
		{
			{
				delete(values, key)
			}
		}
	}

	// The map and the key are compared as syntax: a selector, an index, a dereference, an operation, and a call,
	// which runs twice either way.
	if _, ok := other.values[other.key]; ok {
		delete(other.values, other.key)
	}
	if _, ok := values[keys[0]]; ok {
		delete(values, keys[0])
	}
	if _, ok := values[*pointer]; ok {
		delete(values, *pointer)
	}
	if _, ok := values[key+"suffix"]; ok {
		delete(values, key+"suffix")
	}
	if _, ok := values[(key)+"suffix"]; ok {
		delete(values, key+("suffix"))
	}
	if _, ok := values[compute()]; ok {
		delete(values, compute())
	}

	// The fix prints the call from its own positions, over its lines, and the comments in the if go with it.
	if _, ok := values[key]; ok {
		// The guard says nothing delete doesn't.
		delete(values, key) // Gone.
	}
	if _, ok := values[key]; ok {
		delete(
			values,
			key,
		)
	}
	if _, ok := values[key]; ok {
		delete(values /* the key */, key)
	}
	// An empty else is a block with no statements, whose nil list the pattern's `nil` matches.
	if _, ok := values[key]; ok {
		delete(values, key)
	} else {
	}
	if _, ok := values[key]; ok {
		{
			delete(values, key)
		}
	} else {
		// A comment is no statement.
	}
}

func composite(points map[point]int, arrays map[[2]int]int, boxes map[any]int, conversions map[int64]int, number int) {
	if _, ok := points[point{1, 2}]; ok {
		delete(points, point{1, 2})
	}
	if _, ok := points[point{x: 1}]; ok {
		delete(points, point{x: 1})
	}
	if _, ok := arrays[[2]int{1, 2}]; ok {
		delete(arrays, [2]int{1, 2})
	}
	if _, ok := arrays[[...]int{1, 2}]; ok {
		delete(arrays, [...]int{1, 2})
	}
	if _, ok := conversions[int64(number)]; ok {
		delete(conversions, int64(number))
	}
	if _, ok := boxes[any(number)]; ok {
		delete(boxes, any(number))
	}
	if _, ok := boxes[interface{}(number)]; ok {
		delete(boxes, interface{}(number))
	}
}

func generic[Map ~map[Key]Value, Key comparable, Value any](values Map, key Key) {
	if _, ok := values[key]; ok {
		delete(values, key)
	}
}

func nested(values map[string]int, key string) {
	if key != "" {
		if _, ok := values[key]; ok {
			delete(values, key)
		}
	}
	function := func() {
		if _, ok := values[key]; ok {
			delete(values, key)
		}
	}
	function()
}

// Names are compared by their spelling alone, so the key the lookup reads and the key delete reads are one here,
// though the second is the found flag the initializer declares, which shadows the first.
func spelledAlike(flags map[bool]int, key bool) {
	if _, key := flags[key]; key {
		delete(flags, key)
	}
}

func spreadCall(counts map[int]int, values []any) {
	// A call's spread is a position the comparison never reads, so a spread call is the same as one without.
	if _, ok := counts[spread(values...)]; ok {
		delete(counts, spread(values))
	}
	if _, ok := counts[spread(values...)]; ok {
		delete(counts, spread(values...))
	}
	// A function literal with no parameters and no results is the same as itself, statements and all.
	if _, ok := counts[run(func() {})]; ok {
		delete(counts, run(func() {}))
	}
	if _, ok := counts[run(func() { _ = 1 })]; ok {
		delete(counts, run(func() { _ = 1 }))
	}
}

func notReported(values map[string]int, other map[string]int, key string, second string, keys []string, channel chan int) {
	var ok bool
	// `=` is not `:=`.
	if _, ok = values[key]; ok {
		delete(values, key)
	}
	// The first name must be the blank identifier.
	if value, ok := values[key]; ok {
		_ = value
		delete(values, key)
	}
	if value, ok := values[key]; ok && value > 0 {
		delete(values, key)
	}
	if found, _ := values[key]; found > 0 {
		delete(values, key)
	}
	// The condition must be the found flag alone.
	if _, ok := values[key]; !ok {
		delete(values, key)
	}
	if _, found := values[key]; ok {
		_ = found
		delete(values, key)
	}
	if _, ok := values[key]; ok == true {
		delete(values, key)
	}
	// The body must be the one call, and no else may follow but an empty one, and the fix replaces that too.
	if _, ok := values[key]; ok {
		println("deleting")
		delete(values, key)
	}
	if _, ok := values[key]; ok {
	}
	if _, ok := values[key]; ok {
		delete(values, key)
	} else {
		println("kept")
	}
	if _, ok := values[key]; ok {
		delete(values, key)
	} else if key == "" {
		println("empty")
	}
	// The else if is an if statement of its own, which is reported.
	if _, ok := values[key]; ok {
		delete(values, key)
	} else if _, ok := values[key]; ok {
		delete(values, key)
	}
	if _, ok := values[key]; ok {
		{
		}
		delete(values, key)
	}
	// The call must be delete's, on the same map and the same key, as syntax.
	if _, ok := values[key]; ok {
		delete(other, key)
	}
	if _, ok := values[key]; ok {
		delete(values, second)
	}
	if _, ok := values[key]; ok {
		delete(values, "literal")
	}
	if _, ok := values[keys[0]]; ok {
		delete(values, keys[1])
	}
	if _, ok := values["\x61"]; ok {
		delete(values, "a")
	}
	if _, ok := values[key]; ok {
		_ = len(values)
	}
	// Other two-result initializers are not a lookup.
	var boxed any = key
	if _, ok := boxed.(string); ok {
		delete(values, key)
	}
	if _, ok := <-channel; ok {
		delete(values, key)
	}
	// No initializer, or one that is not an assignment.
	if len(values) > 0 {
		delete(values, key)
	}
	if println(); ok {
		delete(values, key)
	}
}

func literalSpellings(counts map[int]int, floats map[float64]int) {
	// Literals are compared by their text and kind, so a value spelled two ways is two.
	if _, ok := counts[0x1]; ok {
		delete(counts, 1)
	}
	if _, ok := floats[1]; ok {
		delete(floats, 1.0)
	}
	if _, ok := counts['a']; ok {
		delete(counts, 97)
	}
}

func tagless(points map[struct{ x int }]int, tagged map[struct {
	x int "tag"
}]int, counts map[int]int) {
	// A field with no tag is never the same as another field in match.go, so a struct type that declares one, and a
	// function type with a result, is not the same as itself.
	if _, ok := points[struct{ x int }{1}]; ok {
		delete(points, struct{ x int }{1})
	}
	if _, ok := tagged[struct {
		x int "tag"
	}{1}]; ok {
		delete(tagged, struct {
			x int "tag"
		}{1})
	}
	if _, ok := counts[run(func() { _ = func() int { return 1 } })]; ok {
		delete(counts, run(func() { _ = func() int { return 1 } }))
	}
}

func shadowedInBlock(values map[string]int, key string) {
	delete := func(values map[string]int, key string) {}
	if _, ok := values[key]; ok {
		delete(values, key)
	}
}
