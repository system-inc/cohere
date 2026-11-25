package allowed

import (
	"strconv"
)

// earlyReturn is the shape the rule asks for: the branch returns and the rest of the function needs no else.
func earlyReturn(ready bool) int {
	if ready {
		return 1
	}
	return 2
}

// branchFallsThrough has an else after a branch that does not return, so the else is what keeps the two apart.
func branchFallsThrough(ready bool) int {
	total := 0
	if ready {
		total++
	} else {
		return total
	}
	return total
}

// returnOnlyOnSomePaths returns from an if inside the branch that has no else, so the branch can fall through.
func returnOnlyOnSomePaths(left bool, right bool) int {
	if left {
		if right {
			return 1
		}
	} else {
		return 2
	}
	return 3
}

// nestedChainWithoutFinalElse ends its branch in an else-if chain that has no final else, so it can fall through.
func nestedChainWithoutFinalElse(left bool, right bool, other bool) int {
	if left {
		if right {
			return 1
		} else if other {
			return 2
		}
	} else {
		return 3
	}
	return 4
}

// middleBranchFallsThrough has a chain whose middle branch does not return, so the final else still matters.
func middleBranchFallsThrough(count int) string {
	label := ""
	if count == 0 {
		return "none"
	} else if count == 1 {
		label = "one"
	} else {
		label = "many"
	}
	return label
}

// chainWithoutFinalElse returns from every branch but has no else to drop, and an else-if is allowed.
func chainWithoutFinalElse(count int) string {
	if count == 0 {
		return "none"
	} else if count == 1 {
		return "one"
	}
	return "many"
}

// elseIfAfterReturn has an else-if after a returning branch, which the core port's default allows.
func elseIfAfterReturn(count int) string {
	if count == 0 {
		return "none"
	} else if count > 0 {
		count--
	}
	return strconv.Itoa(count)
}

// elseReadsInitVariable reads in its else what the init statement declared. Dropping the else would mean moving the
// declaration out of the if and widening its scope, a choice the author made on purpose, so it is left alone.
func elseReadsInitVariable(text string) int {
	if value, parseError := strconv.Atoi(text); parseError != nil {
		return 0
	} else {
		return value
	}
}

// elseReadsLaterInitVariable reads a variable that an else-if's init statement declared, which is in scope in the
// final else and nowhere after the chain.
func elseReadsLaterInitVariable(text string, empty bool) int {
	if empty {
		return 0
	} else if value, parseError := strconv.Atoi(text); parseError != nil {
		return -1
	} else {
		return value
	}
}

// elseReadsInitVariableInClosure reads the init variable inside a function literal, which still needs it in scope.
func elseReadsInitVariableInClosure(text string) func() int {
	if value, parseError := strconv.Atoi(text); parseError != nil {
		return nil
	} else {
		return func() int { return value }
	}
}

// labeledIf is held by a label, which the core port does not judge since a label holds one statement.
func labeledIf(ready bool) int {
	goto check
check:
	if ready {
		return 1
	} else {
		return 2
	}
}

// noElse at all has nothing to report.
func noElse(ready bool) int {
	if ready {
		return 1
	}
	if !ready {
		return 2
	}
	return 3
}
