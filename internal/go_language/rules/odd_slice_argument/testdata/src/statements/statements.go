// Package statements passes odd slices to a marked parameter from go and defer statements, whose call's source is the
// statement, where the original panics reading it as a call expression; only the port runs here.
package statements

func needsPairs(values ...int) {
	if len(values)%2 != 0 {
		panic("odd")
	}
}

// statements holds the records, each ruled in this doc comment: the port rightly reports the odd argument inside
// each statement's call, the go's three values and the defer's one, where the original panics and loses the package,
// and not the go's even two.
func statements() {
	go needsPairs(1, 2, 3) // port finds: variadic argument "values" is expected to have even number of elements, but has 3 elements
	defer needsPairs(1)    // port finds: variadic argument "values" is expected to have even number of elements, but has 1 elements
	go needsPairs(1, 2)
}
