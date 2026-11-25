// Package parity holds the shapes the pattern `(CallExpr (Builtin "append") [_])` accepts and refuses: parentheses on
// the function, the argument and the call, the argument counts around one, an append the code declares, and the kinds
// of slice append takes. Each function's comment says what the original makes of it; the parity test holds only that
// the port makes the same.
package parity

type integers []int

type holder struct{}

func (holder) append(values []int) []int { return values }

func use(values ...any) {}

func pair() ([]int, int) { return nil, 0 }

// Found: package-level, in a variable's initializer.
var global = append([]int{})

// Found: the plain shape.
func plain(values []int) {
	values = append(values)
	use(values)
}

// Found: the function in parentheses.
func parenthesizedFunction(values []int) {
	values = (append)(values)
	use(values)
}

// Found: the argument in parentheses.
func parenthesizedArgument(values []int) {
	values = append((values))
	use(values)
}

// Found: the whole call in parentheses.
func parenthesizedCall(values []int) {
	values = (append(values))
	use(values)
}

// Found: a named slice type.
func named(values integers) {
	values = append(values)
	use(values)
}

// Found: a slice of a type parameter.
func generic[Value any](values []Value) {
	values = append(values)
	use(values)
}

// Found: a slice whose type is a type parameter.
func genericSlice[Slice ~[]int](values Slice) {
	values = append(values)
	use(values)
}

// Found: the one argument is a call returning a slice and a value to append, which does append it. The pattern counts
// the syntax's arguments, so the original reports this one, wrongly, and the port keeps it.
func multipleResults() {
	values := append(pair())
	use(values)
}

// Found: a call as the one argument, a literal slice, and inside a closure.
func shapes() {
	use(append([]int{1}), append(make([]int, 2)))
	func() {
		use(append([]string(nil)))
	}()
}

// Not found: two or more arguments, or a spread.
func more(values []int, others []int) {
	values = append(values, 1)
	values = append(values, 1, 2)
	values = append(values, others...)
	use(values, append([]byte("a"), "b"...))
}

// Not found: a parameter named append shadows the builtin.
func shadowedByParameter(append func([]int) []int, values []int) {
	values = append(values)
	use(values)
}

// Not found: a local function named append.
func shadowedByLocal(values []int) {
	append := func(values []int) []int { return values }
	values = append(values)
	use(values)
}

// Not found: a method named append.
func method(target holder, values []int) {
	values = target.append(values)
	use(values)
}
