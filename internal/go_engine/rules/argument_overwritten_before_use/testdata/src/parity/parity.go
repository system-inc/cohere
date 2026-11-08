// Package parity holds the shapes of parameter the check reads, read, overwritten, captured or never touched, and the
// shapes of assignment it looks for, where the port and staticcheck's SA4009 still agree finding for finding, related
// information included. The differential test runs both over each and compares.
package parity

type counter struct{ count int }

func compute() (int, error) { return 3, nil }

func consume(values ...int) {}

// Found by both: the parameter is overwritten before anything reads it.
func overwritten(value int) int {
	value = 1
	return value
}

// Found by both: the second parameter is overwritten, the first read.
func secondOverwritten(first int, second int) int {
	second = first
	return second
}

// Found by both, twice: two parameters of one field, overwritten by one assignment, related to it both times.
func groupedOverwritten(first, second int) int {
	first, second = 1, 2
	return first + second
}

// Found by both: overwritten by a call of two results.
func overwrittenByCall(value int) (int, error) {
	value, err := compute()
	return value, err
}

// Found by both: overwritten by a declaration that declares another name beside it.
func overwrittenBesideDeclaration(value int) int {
	other, value := 1, 2
	return other + value
}

// Found by both: overwritten twice, related to the first.
func overwrittenTwice(value int) int {
	value = 1
	value = 2
	return value
}

// Found by both: overwritten inside a branch, the first assignment the syntax reads, and on every path.
func overwrittenInBothBranches(value int, condition bool) int {
	if condition {
		value = 1
	} else {
		value = 2
	}
	return value
}

// Found by both: overwritten in a loop's init.
func overwrittenInLoopInit(value int) {
	for value = 0; value < 3; value++ {
		consume(value)
	}
}

// Found by both: a variadic parameter overwritten.
func variadicOverwritten(values ...int) []int {
	values = nil
	return values
}

// Found by both: a method's parameter.
func (value *counter) method(count int) {
	count = 0
	value.count = count
}

// Found by both: a generic function's parameter.
func genericOverwritten[T any](value T, replacement T) T {
	value = replacement
	return value
}

// Found by both, once: a function literal's parameter, walked from the enclosing function and from the literal.
func literalOverwritten() func(int) int {
	return func(value int) int {
		value = 4
		return value
	}
}

// Found by both, once each: the enclosing function and its literal each overwrite a parameter of the same name.
func outerAndLiteral(value int) int {
	value = 5
	inner := func(value int) int {
		value = 6
		return value
	}
	return inner(value)
}

// Found by both: overwritten and never read again, a value thrown away twice.
func overwrittenUnread(value int) {
	value = 7
	_ = value
}

// Found by neither: the parameter is read before it is overwritten.
func readFirst(value int) int {
	consume(value)
	value = 1
	return value
}

// Found by neither: overwritten on only one path, so the passed value reaches the return on the other.
func overwrittenSometimes(value int, condition bool) int {
	if condition {
		value = 1
	}
	return value
}

// Found by neither: a compound assignment reads the parameter.
func compoundAssignment(value int) int {
	value += 1
	return value
}

// Found by neither: an increment reads the parameter.
func increment(value int) int {
	value++
	return value
}

// Found by neither: the parameter is never read and never assigned.
func unused(value int) {}

// Found by neither: the parameter is captured by a literal that overwrites it, so it lives in memory.
func capturedOverwritten(value int) int {
	overwrite := func() { value = 1 }
	overwrite()
	return value
}

// Found by neither: the parameter's address is taken before it is overwritten.
func addressTaken(value int) *int {
	pointer := &value
	value = 1
	return pointer
}

// Found by neither: a write through a pointer parameter reads the pointer.
func throughPointer(pointer *int) {
	*pointer = 1
}

// Found by neither: a write to a field of a parameter reads the parameter.
func toField(value counter) counter {
	value.count = 1
	return value
}

// Found by neither: overwritten by a range clause, which is no assignment statement.
func overwrittenByRange(value int, values []int) {
	for value = range values {
		consume(value)
	}
}

// Found by neither: an inner declaration of the same name is another variable.
func shadowed(value int) {
	{
		value := 1
		consume(value)
	}
}

// Found by neither: the receiver is overwritten, and receivers are no parameters to the check.
func (value counter) receiverOverwritten() counter {
	value = counter{}
	return value
}

// Found by neither: a named result is overwritten, and results are no parameters either.
func resultOverwritten() (result int) {
	result = 1
	return result
}

// Found by neither: the parameters have no names.
func unnamed(int, string) {}

// Found by neither: a function with no parameters.
func noParameters() int {
	value := 1
	value = 2
	return value
}
