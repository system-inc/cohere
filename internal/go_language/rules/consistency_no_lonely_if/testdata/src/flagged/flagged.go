package flagged

import (
	"strconv"
)

// plainLonelyIf holds one if in its else and nothing else.
func plainLonelyIf(left bool, right bool) int {
	if left {
		return 1
	} else {
		if right { // want "nothing but an `if`"
			return 2
		}
	}
	return 3
}

// lonelyIfWithElse carries its own else, which an else-if keeps.
func lonelyIfWithElse(left bool, right bool) int {
	total := 0
	if left {
		total = 1
	} else {
		if right { // want "nothing but an `if`"
			total = 2
		} else {
			total = 3
		}
	}
	return total
}

// lonelyIfWithChain carries an else-if chain of its own.
func lonelyIfWithChain(count int) string {
	label := ""
	if count == 0 {
		label = "none"
	} else {
		if count == 1 { // want "nothing but an `if`"
			label = "one"
		} else if count == 2 {
			label = "two"
		}
	}
	return label
}

// endOfChain has the lonely if in the final else of an else-if chain.
func endOfChain(count int) string {
	label := "many"
	if count == 0 {
		label = "none"
	} else if count == 1 {
		label = "one"
	} else {
		if count == 2 { // want "nothing but an `if`"
			label = "two"
		}
	}
	return label
}

// twiceLonely nests a lonely if inside a lonely if's else, and each is reported.
func twiceLonely(count int) string {
	label := "many"
	if count == 0 {
		label = "none"
	} else {
		if count == 1 { // want "nothing but an `if`"
			label = "one"
		} else {
			if count == 2 { // want "nothing but an `if`"
				label = "two"
			}
		}
	}
	return label
}

// withInitStatement declares in the inner if's init statement, which an else-if allows as well.
func withInitStatement(empty bool, text string) int {
	if empty {
		return 0
	} else {
		if value, parseError := strconv.Atoi(text); parseError == nil { // want "nothing but an `if`"
			return value
		}
	}
	return -1
}

// commentAboveInner explains the inner if, and the comment can move up beside the else-if.
func commentAboveInner(left bool, right bool) int {
	if left {
		return 1
	} else {
		// Only when the right side is ready.
		if right { // want "nothing but an `if`"
			return 2
		}
	}
	return 3
}

// commentAfterInner holds a comment after the inner if, which is no statement.
func commentAfterInner(left bool, right bool) int {
	if left {
		return 1
	} else {
		if right { // want "nothing but an `if`"
			return 2
		}
		// Falls through to the default below.
	}
	return 3
}

// inFunctionLiteral is judged the same inside a literal.
func inFunctionLiteral() func(bool, bool) int {
	return func(left bool, right bool) int {
		if left {
			return 1
		} else {
			if right { // want "nothing but an `if`"
				return 2
			}
		}
		return 3
	}
}

// inGenericFunction is judged the same as any other function.
func inGenericFunction[Value comparable](left Value, right Value, zero Value) string {
	if left == right {
		return "same"
	} else {
		if left == zero { // want "nothing but an `if`"
			return "zero"
		}
	}
	return "different"
}

// multiByte keeps runes of more than one byte on the reported lines, so positions are byte offsets that still land.
func multiByte(greeting string) string {
	if greeting == "héllo" {
		return "世界"
	} else { /* 世界 */
		/* ëlse */ if greeting == "wörld" { // want "nothing but an `if`"
			return "ëlse"
		}
	}
	return ""
}
