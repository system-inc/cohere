package flagged

import (
	"errors"
	"strconv"
)

// plainElse returns from its branch and then says else anyway.
func plainElse(ready bool) int {
	if ready {
		return 1
	} else { // want "always returns"
		return 2
	}
}

// elseThatFallsOut does not return from its else, which is still dead weight.
func elseThatFallsOut(ready bool) int {
	total := 0
	if ready {
		return total
	} else { // want "always returns"
		total++
	}
	return total
}

// emptyElse has nothing in its else at all.
func emptyElse(ready bool) int {
	if ready {
		return 1
	} else { // want "always returns"
	}
	return 0
}

// commentOnlyElse holds only a comment, which is still an else to drop.
func commentOnlyElse(ready bool) int {
	if ready {
		return 1
	} else { // want "always returns"
		// Nothing to do here.
	}
	return 0
}

// wholeChainReturns returns from every branch of the chain, so its final else is the one reported.
func wholeChainReturns(count int) string {
	if count == 0 {
		return "none"
	} else if count == 1 {
		return "one"
	} else { // want "always returns"
		return "many"
	}
}

// nestedFullyReturningIf returns through an if and else that both return.
func nestedFullyReturningIf(left bool, right bool) int {
	if left {
		if right {
			return 1
		} else { // want "always returns"
			return 2
		}
	} else { // want "always returns"
		return 3
	}
}

// nestedBlock returns at the end of a bare block.
func nestedBlock(ready bool) int {
	if ready {
		{
			return 1
		}
	} else { // want "always returns"
		return 2
	}
}

// statementsBeforeReturn does work before returning, and only the last statement decides.
func statementsBeforeReturn(text string) (int, error) {
	if text == "" {
		parseError := errors.New("parsing an empty string")
		return 0, parseError
	} else { // want "always returns"
		return strconv.Atoi(text)
	}
}

// initNotUsedInElse declares in its init statement, and the else never reads it, so dropping the else is free.
func initNotUsedInElse(text string) int {
	if value, parseError := strconv.Atoi(text); parseError != nil {
		return value
	} else { // want "always returns"
		return len(text)
	}
}

// initAssignmentOnly assigns in its init statement rather than declaring, so nothing would leave scope.
func initAssignmentOnly(text string) int {
	var value int
	var parseError error
	if value, parseError = strconv.Atoi(text); parseError != nil {
		return 0
	} else { // want "always returns"
		return value
	}
}

// labeledReturn returns through a labeled statement, which a goto names so the label is used.
func labeledReturn(ready bool) int {
	if ready {
		goto done
	done:
		return 1
	} else { // want "always returns"
		return 2
	}
}

// inCaseClause sits in a switch case's statement list.
func inCaseClause(count int, ready bool) int {
	switch count {
	case 1:
		if ready {
			return 1
		} else { // want "always returns"
			return 2
		}
	}
	return 0
}

// inSelectClause sits in a select clause's statement list.
func inSelectClause(channel chan int) int {
	select {
	case value := <-channel:
		if value > 0 {
			return value
		} else { // want "always returns"
			return -value
		}
	}
}

// inFunctionLiteral returns from the literal, which is the function the else belongs to.
func inFunctionLiteral() func(bool) string {
	return func(ready bool) string {
		if ready {
			return "ready"
		} else { // want "always returns"
			return "waiting"
		}
	}
}

// inGenericFunction is judged the same as any other function.
func inGenericFunction[Value comparable](left Value, right Value) string {
	if left == right {
		return "same"
	} else { // want "always returns"
		return "different"
	}
}

// multiByte keeps runes of more than one byte on the reported lines, so positions are byte offsets that still land.
func multiByte(greeting string) string {
	if greeting == "héllo wörld 世界" {
		return "世界"
	} else { /* 世界 */ // want "always returns"
		return "ëlse"
	}
}

// collidingName declares in its else a name the code after the if declares too. Unwrapping needs a rename, which is
// a fix a reader can make, so the else is still reported, as the core port reports it.
func collidingName(ready bool) int {
	if ready {
		return 1
	} else { // want "always returns"
		count := 2
		_ = count
	}
	count := 3
	return count
}
