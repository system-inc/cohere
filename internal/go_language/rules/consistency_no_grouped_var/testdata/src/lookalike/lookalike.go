package lookalike

func use(values ...any) {}

func read() (int, error) { return 0, nil }

// Parentheses around a value are not a grouped block.
func parenthesizedValue() {
	var total = (1 + 2)
	use(total)
}

// A parenthesized multi-return call still yields every name.
func parenthesizedCall() {
	var value, readError = (read())
	use(value, readError)
}

// Names listed in a function type's parameters, a struct's fields or an array's elements belong to the type or value,
// and the var declares one name.
func namesInsideOneDeclaration() {
	var callback func(first, second int)
	var pair struct{ first, second int }
	var both = [2]int{1, 2}
	use(callback, pair, both)
}

// Names listed in parameters and results are a signature, not a declaration.
func parameters(first, second int) (sum, difference int) {
	return first + second, first - second
}

// A range clause and a plain assignment list names without var or const.
func rangeAndAssignment(values map[string]int) {
	for key, value := range values {
		use(key, value)
	}
	var first = 1
	var second = 2
	first, second = second, first
	use(first, second)
}
