// Package differences holds the shapes where the port and the original could most plausibly part, where match.go's
// comparison of two nodes answers by its own order rather than by Go's meaning, and records none, since the port
// answers as the original does at each.
package differences

type state struct {
	values map[string]int
	keys   []string
	key    string
}

func run(function func()) int {
	function()
	return 0
}

func noop() {}

func keyOf(index int) string { return "" }

func deepParentheses(values map[string]int, key string, current *state) {
	// Parentheses deep inside the map and the key, on one side or the other, are looked through.
	if _, ok := (current).values[(current.keys)[0]]; ok {
		delete(current.values, current.keys[(0)])
	}
	if _, ok := values[keyOf((1)+2)]; ok {
		delete((values), keyOf(1+(2)))
	}
	// A field of the map's holder against the holder's map named another way is two.
	if _, ok := current.values[key]; ok {
		delete(values, key)
	}
}

func functionLiterals(counts map[int]int) {
	// A block of one statement in a function literal is the same as that statement, as match.go compares them.
	if _, ok := counts[run(func() {
		{
			return
		}
	})]; ok {
		delete(counts, run(func() { return }))
	}
	// But a block around an expression statement is not, since match.go sheds the statement before it lists it.
	if _, ok := counts[run(func() {
		{
			noop()
		}
	})]; ok {
		delete(counts, run(func() { noop() }))
	}
}

func multilineFix(values map[string]int, current *state) {
	if _, ok := values[current.
		key]; ok {
		// A comment before.
		delete(values,
			current.
				key) // A comment after.
	}
}
