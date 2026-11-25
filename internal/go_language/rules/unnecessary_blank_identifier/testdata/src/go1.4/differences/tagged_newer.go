//go:build go1.23

package differences

// alias is an alias of a function type, whose underlying type is the function's.
type alias = func(yield func(string, int) bool)

// pairs is a map type, whose range has a key and a value.
type pairs map[string]int

// A range over an alias of a function is one over the function, so neither reports there, and a range over a named
// map is one over the map.
func overAlias(sequence alias, table pairs) {
	for _, _ = range sequence {
	}
	var key string
	for key, _ = range sequence {
	}
	for key, _ = range table {
	}
	_ = key
}
