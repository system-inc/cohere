// Package statements passes odd slices to a marked parameter from go and defer statements, whose call's source is the
// statement, where the original panics reading it as a call expression; only the port runs here.
package statements

func needsPairs(values ...int) {
	if len(values)%2 != 0 {
		panic("odd")
	}
}

func statements() {
	go needsPairs(1, 2, 3)
	defer needsPairs(1)
	go needsPairs(1, 2)
}
