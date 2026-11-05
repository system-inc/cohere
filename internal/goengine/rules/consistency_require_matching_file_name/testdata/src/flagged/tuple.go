package flagged

// A generic type is named without its type parameters.
type Pair[Key comparable, Value any] struct { // want "`Pair` is the one type .* Name it `pair.go`.$"
	key   Key
	value Value
}

func (pair Pair[Key, Value]) Key() Key {
	return pair.key
}
