// Package parity holds the shapes of loop the check reads, its condition's variable changed, held in memory, or the
// same on every pass, and the shapes of syntax it refuses, where the port and staticcheck's SA4008 still agree finding
// for finding. The differential test runs both over each and compares.
package parity

var global int

type counter struct{ count int }

func compute() int { return 3 }

func consume(values ...int) {}

// Found by both: the post statement increments another variable.
func wrongIncrement() {
	other := 0
	for index := 0; index < 10; other++ {
	}
	consume(other)
}

// Found by both: the post statement decrements another variable.
func wrongDecrement(limit int) {
	other := limit
	for index := limit; index > 0; other-- {
	}
}

// Found by both: the variable is assigned, not declared, in the init, and stays the constant it was given.
func assignedInit() {
	var index int
	other := 0
	for index = 0; index < 10; other++ {
	}
	consume(index, other)
}

// Found by both: the init gives the variable a parameter, which no pass changes.
func parameterInit(start int) {
	var index int
	other := 0
	for index = start; index < 10; other++ {
	}
}

// Found by both: the init gives the variable the result of a call.
func callInit() {
	other := 0
	for index := compute(); index < 10; other++ {
	}
}

// Found by both: the body returns on every pass, so the post statement never runs and the variable stays 0.
func bodyReturns() int {
	for index := 0; index < 10; index++ {
		return index
	}
	return -1
}

// Found by both: the body panics on every pass.
func bodyPanics() {
	for index := 0; index < 10; index++ {
		panic("once")
	}
}

// Found by both: the body breaks on every pass.
func bodyBreaks() {
	for index := 0; index < 10; index++ {
		consume(index)
		break
	}
}

// Found by both, once: a loop inside a function literal, walked from the enclosing function and from the literal.
func insideLiteral() func() {
	return func() {
		other := 0
		for index := 0; index < 10; other++ {
		}
	}
}

// Found by both, once each: a literal inside a literal, each with a loop of its own.
func nestedLiterals() {
	first := func() {
		other := 0
		for index := 0; index < 3; other++ {
		}
		second := func() {
			other := 0
			for index := 0; index < 3; other++ {
			}
		}
		second()
	}
	first()
}

// Found by both: the condition compares with another operator than less than.
func notEqualCondition() {
	other := 0
	for index := 0; index != 10; other++ {
	}
}

// Found by both: the condition's right side is a call, the left still the unchanged variable.
func callOnTheRight() {
	other := 0
	for index := 0; index < compute(); other++ {
	}
}

// Found by both: a method's loop.
func (value *counter) method() {
	for index := 0; index < value.count; value.count++ {
	}
}

// Found by both: a generic function's loop.
func genericLoop[T any](values []T) {
	other := 0
	for index := 0; index < len(values); other++ {
	}
}

// Found by both: the outer loop is fine, the inner one increments the outer's variable, upstream's shape.
func nestedWrongIncrement() {
	for outer := 0; outer < 10; outer++ {
		for inner := 0; inner < 10; outer++ {
		}
	}
}

// Found by both: a string, not a number, compared in the condition.
func stringCondition() {
	other := 0
	for text := "a"; text < "b"; other++ {
	}
}

// Found by neither: an ordinary counting loop, its variable a phi at the head.
func ordinary() {
	for index := 0; index < 10; index++ {
		consume(index)
	}
}

// Found by neither: the post statement increments another variable, but the body changes the condition's.
func changedInBody() {
	other := 0
	for index := 0; index < 10; other++ {
		index++
	}
}

// Found by neither: the body changes the variable on only some passes, still a phi.
func changedSometimes(limit int) {
	other := 0
	for index := 0; index < 10; other++ {
		if other > limit {
			index = 10
		}
	}
}

// Found by neither: the variable's address is taken, so the condition loads it afresh on every pass, upstream's shape.
func addressTaken() {
	other := 0
	for index := 0; index < 10; other++ {
		pointer := &index
		*pointer++
	}
}

// Found by neither: the variable is captured by a function literal, so it lives in memory and is loaded.
func captured() {
	other := 0
	for index := 0; index < 10; other++ {
		increment := func() { index++ }
		increment()
	}
}

// Found by neither: the variable is a package global, loaded on every pass.
func globalVariable() {
	other := 0
	for global = 0; global < 10; other++ {
	}
}

// Found by neither: the post statement is an assignment, not an increment.
func assignmentPost() {
	other := 0
	for index := 0; index < 10; other += 1 {
	}
}

// Found by neither: the condition's variable is on the right of the comparison.
func variableOnTheRight() {
	other := 0
	for index := 0; 10 > index; other++ {
	}
}

// Found by neither: the condition's variable is in parentheses.
func parenthesizedVariable() {
	other := 0
	for index := 0; (index) < 10; other++ {
	}
}

// Found by neither: the condition is not a comparison but a boolean variable.
func booleanCondition() {
	other := 0
	for done := false; !done; other++ {
	}
}

// Found by neither: the condition compares a different variable from the one the init declares.
func differentVariable(limit int) {
	other := 0
	for index := 0; other < limit; other++ {
		consume(index)
	}
}

// Found by neither: the init declares two variables.
func twoInitVariables() {
	for index, other := 0, 0; index < 10; other++ {
	}
}

// Found by neither: the init is a call, not an assignment.
func statementInit() {
	index := 0
	other := 0
	for consume(); index < 10; other++ {
	}
}

// Found by neither: the loop has no init.
func noInit() {
	index := 0
	other := 0
	for ; index < 10; other++ {
	}
}

// Found by neither: the loop has no post statement.
func noPost() {
	for index := 0; index < 10; {
		index++
	}
}

// Found by neither: the loop has no condition.
func noCondition() {
	other := 0
	for index := 0; ; other++ {
		if index > other {
			return
		}
	}
}

// Found by neither: the init assigns a field, not a name.
func fieldInit(value *counter) {
	other := 0
	for value.count = 0; value.count < 10; other++ {
	}
}

// Found by neither: the condition reads a field of the declared variable, not the variable.
func fieldCondition() {
	other := 0
	for value := (counter{}); value.count < 10; other++ {
	}
}

// Found by neither: the body continues on every pass, and the post statement still increments.
func bodyContinues() {
	for index := 0; index < 10; index++ {
		continue
	}
}

// Found by neither: a range loop, which is no three-clause loop.
func rangeLoop(values []int) {
	for index := range values {
		consume(index)
	}
}
