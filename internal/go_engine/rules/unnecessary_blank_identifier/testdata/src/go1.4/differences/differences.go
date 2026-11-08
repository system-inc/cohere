package differences

// table is a named map, whose range is a map's.
type table map[string]int

// The shapes where the port and the original could most plausibly part, where they agree.
func ranges(m table, values func() []int) {
	// A parenthesized blank is not a blank as the range judges it: a lone key is not reported, a parenthesized key
	// before a blank value is a key like any other, and a parenthesized value is not a blank.
	for (_) = range m {
	}
	for (_), _ = range m {
	}
	var key string
	for key, (_) = range m {
	}
	// A definition's token is two bytes, and the deletion after a key goes no further than the value's end.
	for index, _ := range m {
		_ = index
	}
	// A range over a call is a range over what it returns.
	for _ = range values() {
	}
	for index, _ := range values() {
		_ = index
	}
	_ = key
}

func receives(ch <-chan int, both chan int) {
	var x int
	// A receive-only channel, a parenthesized operand, a labeled statement and a function literal.
	x, _ = <-ch
	_ = <-(both)
	(x), _ = <-ch
labeled:
	_ = <-ch
	func() {
		x, _ = <-both
	}()
	// The blank alone on the second left side of two blanks is reported as the comma-ok form, and fixed to one blank.
	_, _ = <-ch
	_ = x
	goto labeled
}
