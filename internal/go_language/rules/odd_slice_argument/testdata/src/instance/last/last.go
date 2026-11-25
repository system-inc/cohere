// Package last is package first's functions with loose declared, and instantiated, before pairs. Each function reads as
// it does there, and the port finds what it finds there: pairs and elements need their slices even, and loose does
// not (#8d9r2rh).
//
// staticcheck reads the mark off the instantiation's signature, which pairs[int] and loose[int] share, and here it is
// loose's, instantiated first. So pairs[int] carries loose's parameter as its own, and passing it on to pairs, which
// checks it, marks loose's parameter. staticcheck then reports both slices under the name others, the one loose never
// checks and the one pairs does, where in package first it reported both under the name values.
package last

// loose takes others of any length. Its type parameter is unused.
func loose[Unused any](others []string) {
	_ = len(others)
}

// pairs panics unless its values come in pairs. Its type parameter is unused.
func pairs[Unused any](values []string) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// elements panics unless its values, of its type parameter, come in pairs.
func elements[Element any](values []Element) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// odd passes each a slice of one, loose first, and so instantiates them in that order. The records are ruled here. The port rightly leaves loose's slice
// alone, which loose never checks. The port rightly names pairs's own parameter, values, in the finding both make at
// pairs's slice, where staticcheck names others, the parameter of loose that pairs[int] carries. The port rightly
// reports elements's slice, which elements checks in every instantiation, and staticcheck misses it.
func odd() {
	// A false positive: loose checks nothing, and its parameter is marked only through pairs's instantiation.
	loose[int]([]string{"one"}) // port drops: argument "others" is expected to have even number of elements, but has 1 elements
	pairs[int]([]string{"one"}) // port renames: argument "others" is expected to have even number of elements, but has 1 elements => argument "values" is expected to have even number of elements, but has 1 elements
	elements[int]([]int{1})     // port finds: argument "values" is expected to have even number of elements, but has 1 elements
}
