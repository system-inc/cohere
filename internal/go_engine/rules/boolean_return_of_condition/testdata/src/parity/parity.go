// Package parity holds every shape S1008 branches on and the near miss of each, for the original and the port to
// agree on finding for finding.
package parity

type record struct {
	ready   bool
	items   []bool
	flags   map[string]bool
	signal  chan bool
	pointer *bool
}

func (each record) isReady() bool { return each.ready }

func generic[Value any](value Value) bool { return true }

type truth bool

const zero = 0

// Each comparison, returning true first, is returned as it is.

func equal(a, b int) bool {
	if a == b {
		return true
	}
	return false
}

func notEqual(a, b int) bool {
	if a != b {
		return true
	}
	return false
}

func less(a, b int) bool {
	if a < b {
		return true
	}
	return false
}

func greater(a, b int) bool {
	if a > b {
		return true
	}
	return false
}

func lessOrEqual(a, b int) bool {
	if a <= b {
		return true
	}
	return false
}

func greaterOrEqual(a, b int) bool {
	if a >= b {
		return true
	}
	return false
}

// Each comparison, returning false first, is returned reversed.

func negatedEqual(a, b int) bool {
	if a == b {
		return false
	}
	return true
}

func negatedNotEqual(a, b int) bool {
	if a != b {
		return false
	}
	return true
}

func negatedLess(a, b int) bool {
	if a < b {
		return false
	}
	return true
}

func negatedGreater(a, b int) bool {
	if a > b {
		return false
	}
	return true
}

func negatedLessOrEqual(a, b int) bool {
	if a <= b {
		return false
	}
	return true
}

func negatedGreaterOrEqual(a, b int) bool {
	if a >= b {
		return false
	}
	return true
}

// The message is wrong here, kept as staticcheck's text word for word: on floats, a reversed comparison is not the
// if's opposite, since with a NaN on either side the if returns true and `a >= b` is false; `!(a < b)` would be.
func negatedFloatLess(a, b float64) bool {
	if a < b {
		return false
	}
	return true
}

func negatedSplit(a, b int) bool {
	if a ==
		b {
		return false
	}
	return true
}

// A builtin that never returns a negative number, greater than an integer literal zero, is negated to equal.

func lengthPositive(text string) bool {
	if len(text) > 0 {
		return false
	}
	return true
}

func capacityPositive(items []int) bool {
	if cap(items) > 0 {
		return false
	}
	return true
}

func copiedPositive(destination, source []int) bool {
	if copy(destination, source) > 0 {
		return false
	}
	return true
}

func lengthOfMap(flags map[string]bool) bool {
	if len(flags) > 0 {
		return false
	}
	return true
}

func lengthParenthesizedZero(text string) bool {
	if len(text) > (0) {
		return false
	}
	return true
}

func lengthNegativeZero(text string) bool {
	if len(text) > -0 {
		return false
	}
	return true
}

func lengthHexadecimalZero(text string) bool {
	if len(text) > 0x0 {
		return false
	}
	return true
}

func copiedFromString(destination []byte, sources ...string) bool {
	if copy(destination, sources[0]) > 0 {
		return false
	}
	return true
}

// Near misses of it, negated to less or equal.

func lengthParenthesizedCall(text string) bool {
	if (len(text)) > 0 {
		return false
	}
	return true
}

func lengthOne(text string) bool {
	if len(text) > 1 {
		return false
	}
	return true
}

func lengthNamedZero(text string) bool {
	if len(text) > zero {
		return false
	}
	return true
}

func lengthFloatZero(text string) bool {
	if len(text) > 0.0 {
		return false
	}
	return true
}

func lengthConverted(text string) bool {
	if uint(len(text)) > 0 {
		return false
	}
	return true
}

func lengthShadowed(text string) bool {
	len := func(string) int { return 0 }
	if len(text) > 0 {
		return false
	}
	return true
}

func lengthOnTheRight(text string) bool {
	if 0 < len(text) {
		return false
	}
	return true
}

func lengthPositiveInteger(text string) bool {
	if len(text) > 0 {
		return true
	}
	return false
}

// Every other kind of condition, negated.

func identifier(value bool) bool {
	if value {
		return false
	}
	return true
}

func call() bool {
	if generic(1) {
		return false
	}
	return true
}

func method(each record) bool {
	if each.isReady() {
		return false
	}
	return true
}

func instantiated() bool {
	if generic[int](1) {
		return false
	}
	return true
}

func literalCall() bool {
	if func() bool { return true }() {
		return false
	}
	return true
}

func index(each record) bool {
	if each.items[0] {
		return false
	}
	return true
}

func mapIndex(each record) bool {
	if each.flags["key"] {
		return false
	}
	return true
}

func star(each record) bool {
	if *each.pointer {
		return false
	}
	return true
}

func not(value bool) bool {
	if !value {
		return false
	}
	return true
}

func notOfParentheses(a, b bool) bool {
	if !(a && b) {
		return false
	}
	return true
}

func doubleNot(value bool) bool {
	if !!value {
		return false
	}
	return true
}

func receive(each record) bool {
	if <-each.signal {
		return false
	}
	return true
}

func selector(each record) bool {
	if each.ready {
		return false
	}
	return true
}

func assertion(value any) bool {
	if value.(bool) {
		return false
	}
	return true
}

func booleanComparison(a, b, c bool) bool {
	if (a == b) == c {
		return false
	}
	return true
}

// The message is wrong here, kept as staticcheck's text word for word: it says 'return value', which does not compile,
// since a bool is not assignable to the defined truth; an untyped comparison in its place would be.
func namedResult(value bool) truth {
	if value {
		return true
	}
	return false
}

func parenthesizedResults(value bool) bool {
	if value {
		return (false)
	}
	return (true)
}

// Conditions of any other binary operator are left alone, whichever comes first.

func and(a, b bool) bool {
	if a && b {
		return true
	}
	return false
}

func or(a, b bool) bool {
	if a || b {
		return false
	}
	return true
}

// The same value twice is left alone.

func trueTwice(value bool) bool {
	if value {
		return true
	}
	return true
}

func falseTwice(value bool) bool {
	if value {
		return false
	}
	return false
}

// An if of more than its condition and body is left alone.

func withInit(value bool) bool {
	if inner := value; inner {
		return true
	}
	return false
}

func withElse(value bool) bool {
	if value {
		return true
	} else {
		return false
	}
	return false
}

func withElseIf(a, b bool) bool {
	if a {
		return true
	} else if b {
		return false
	}
	return false
}

func withTwoStatements(value bool) bool {
	if value {
		value = !value
		return true
	}
	return false
}

// Returns of anything but one predeclared true or false are left alone.

func twoResults(value bool) (bool, error) {
	if value {
		return true, nil
	}
	return false, nil
}

func bareReturn(value bool) (result bool) {
	if value {
		return true
	}
	return
}

func variableReturned(value bool) bool {
	if value {
		return true
	}
	return value
}

func notReturning(value bool) bool {
	if value {
		return true
	}
	panic("unreachable")
}

func shadowedFalse(value bool) bool {
	false := true
	if value {
		return true
	}
	return false
}

// A series of ifs is left alone, and an if before the pair that is anything else is no series.

func series(a, b bool) bool {
	if a {
		return false
	}
	if b {
		return true
	}
	return false
}

func afterAssignment(a, b bool) bool {
	a = !a
	if b {
		return true
	}
	return false
}

func afterFor(a bool) bool {
	for range 3 {
	}
	if a {
		return true
	}
	return false
}

// Blocks anywhere are judged, and a case clause's body is no block.

func nested(a, b bool) bool {
	if a {
		if b {
			return true
		}
		return false
	}
	return true
}

func literal() func(bool) bool {
	return func(value bool) bool {
		if value {
			return false
		}
		return true
	}
}

func caseClause(value bool, choice int) bool {
	switch choice {
	case 1:
		if value {
			return true
		}
		return false
	}
	return true
}

func oneStatement(value bool) bool {
	return value
}

// A comment on either statement leaves the pair alone, a staticcheck note excepted.

func commentBefore(value bool) bool {
	// Before the if.
	if value {
		return true
	}
	return false
}

func commentInCondition(value bool) bool {
	if /* inside */ value {
		return true
	}
	return false
}

func commentInBody(value bool) bool {
	if value {
		return true // inside
	}
	return false
}

func commentAfterReturn(value bool) bool {
	if value {
		return true
	}
	return false // after
}

func commentBlock(value bool) bool {
	if value { /*@ diag */
		return true
	}
	return false
}

func commentNote(value bool) bool {
	if value { //@ diag(`a note, ignored`)
		return true
	}
	return false
}

func commentNoteAndAnother(value bool) bool {
	if value { //@ diag(`a note, ignored`)
		return true
	}
	// Another.
	return false
}

func commentAtTheEnd(value bool) bool {
	if value {
		return true
	}
	return false
	// After everything.
}

func commentAfterTheFunction(value bool) bool {
	if value {
		return true
	}
	return false
} // After the function.

func commentBeforeThePair(a, b bool) bool {
	a = !a
	// Between the two.

	if b {
		return true
	}
	return false
}
