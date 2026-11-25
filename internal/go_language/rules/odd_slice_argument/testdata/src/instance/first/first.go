// Package first passes slices of one to three generic functions through their instantiations, the one that checks its
// slice instantiated before the one that checks nothing, and package last, beside it, is the same functions with those
// two the other way round (#8d9r2rh). A call names a declared function and a parameter's place in it, so the port
// asks the mark of that function's own parameter, and finds the same in both packages: pairs and elements need their
// slices even, and loose does not.
//
// staticcheck reads the mark off the instantiation's signature instead. An instantiation's signature is made
// canonical by type identity, so pairs[int] and loose[int], both a function of one slice of strings, share the
// signature of whichever was instantiated first, and with it that function's parameter. Here that is pairs's, so
// staticcheck reports loose's slice by pairs's check. And elements[int]'s parameter, a slice of the type argument, is
// one no function declares, which nothing marks, so staticcheck never reports a slice passed to elements.
package first

// pairs panics unless its values come in pairs. Its type parameter is unused.
func pairs[Unused any](values []string) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// loose takes others of any length. Its type parameter is unused.
func loose[Unused any](others []string) {
	_ = len(others)
}

// elements panics unless its values, of its type parameter, come in pairs.
func elements[Element any](values []Element) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// odd passes each a slice of one, pairs first, and so instantiates them in that order. The records are ruled here. The port rightly leaves loose's slice
// alone, which loose never checks: staticcheck reports it only because loose[int] carries the parameter of pairs,
// instantiated before it, and names that parameter, values, where loose's is others. The port rightly reports
// elements's slice, which elements checks in every instantiation, and staticcheck misses it.
func odd() {
	pairs[int]([]string{"one"})
	// A false positive: loose checks nothing, and the parameter named is pairs's.
	loose[int]([]string{"one"}) // port drops: argument "values" is expected to have even number of elements, but has 1 elements
	elements[int]([]int{1})     // port finds: argument "values" is expected to have even number of elements, but has 1 elements
}
