package allowed

import (
	"strconv"
)

// elseIf is the shape the rule asks for.
func elseIf(left bool, right bool) int {
	if left {
		return 1
	} else if right {
		return 2
	}
	return 3
}

// elseIfWithInit declares in the else-if's init statement.
func elseIfWithInit(empty bool, text string) int {
	if empty {
		return 0
	} else if value, parseError := strconv.Atoi(text); parseError == nil {
		return value
	}
	return -1
}

// statementAfterIf does more in the else than the if, so an else-if could not hold it.
func statementAfterIf(left bool, right bool) int {
	total := 0
	if left {
		total = 1
	} else {
		if right {
			total = 2
		}
		total++
	}
	return total
}

// statementBeforeIf does work before the if, which an else-if has no place for.
func statementBeforeIf(left bool, text string) int {
	total := 0
	if left {
		total = 1
	} else {
		total = len(text)
		if total > 3 {
			total = 3
		}
	}
	return total
}

// twoIfs holds two ifs in the else, which is no single else-if.
func twoIfs(left bool, right bool, other bool) int {
	total := 0
	if left {
		total = 1
	} else {
		if right {
			total = 2
		}
		if other {
			total = 3
		}
	}
	return total
}

// plainElse holds no if at all.
func plainElse(left bool) int {
	total := 0
	if left {
		total = 1
	} else {
		total = 2
	}
	return total
}

// emptyElse holds nothing.
func emptyElse(left bool) int {
	total := 0
	if left {
		total = 1
	} else {
	}
	return total
}

// noElse has nothing to judge.
func noElse(left bool) int {
	if left {
		return 1
	}
	return 2
}
