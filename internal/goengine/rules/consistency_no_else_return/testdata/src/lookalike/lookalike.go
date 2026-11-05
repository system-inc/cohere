package lookalike

import (
	"os"
)

// panicBranch ends in a panic, which never returns, but the rule is about return and does not read it.
func panicBranch(ready bool) int {
	if ready {
		panic("not ready")
	} else {
		return 1
	}
}

// exitBranch ends in os.Exit, which is a call like any other to the rule.
func exitBranch(ready bool) int {
	if ready {
		os.Exit(1)
	} else {
		return 1
	}
	return 0
}

// continueBranch ends in continue, which leaves the iteration rather than the function.
func continueBranch(values []int) int {
	total := 0
	for _, value := range values {
		if value < 0 {
			continue
		} else {
			total += value
		}
	}
	return total
}

// gotoBranch ends in a goto, which the rule does not trace.
func gotoBranch(ready bool) int {
	if ready {
		goto done
	} else {
		return 1
	}
done:
	return 0
}

// returnInsideLiteral returns from a function literal, not from the function the if belongs to, so the branch falls
// through after the call.
func returnInsideLiteral(ready bool) int {
	total := 0
	if ready {
		func() {
			total++
			return
		}()
	} else {
		total--
	}
	return total
}

// returnInsideLoop returns from inside a loop, which may run no times, so the branch can fall through.
func returnInsideLoop(ready bool, values []int) int {
	if ready {
		for _, value := range values {
			return value
		}
	} else {
		return -1
	}
	return 0
}

// switchThatReturns returns from every clause of a switch with a default, which the rule does not read: a switch
// would need its every break traced, and missing an else is better than flagging one that is needed.
func switchThatReturns(ready bool, count int) int {
	if ready {
		switch count {
		case 0:
			return 0
		default:
			return 1
		}
	} else {
		return 2
	}
}

// returnNotLast has a return before its last statement, unreachable code after it, and the rule reads a branch by
// its last statement as the spec reads a terminating block.
func returnNotLast(ready bool) int {
	total := 0
	if ready {
		return total
		total++
	} else {
		total--
	}
	return total
}

// elseIfNotElse has an else-if in the place an else would sit, and the chain has no final else.
func elseIfNotElse(ready bool, other bool) int {
	if ready {
		return 1
	} else if other {
		return 2
	}
	return 3
}
