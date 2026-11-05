package lookalike

// thenBranch holds one if in its then branch, which is the same shape and not the rule's subject.
func thenBranch(left bool, right bool) int {
	if left {
		if right {
			return 1
		}
	} else {
		return 2
	}
	return 3
}

// thenBranchWithoutElse holds one if in a then branch with no else at all.
func thenBranchWithoutElse(left bool, right bool) int {
	if left {
		if right {
			return 1
		}
	}
	return 2
}

// labeledIf is held by a label a goto names, and an else-if has no place to put a label.
func labeledIf(left bool, right bool) int {
	total := 0
	if left {
		total = 1
	} else {
	again:
		if right {
			total++
			right = total < 3
			goto again
		}
	}
	return total
}

// nestedBlock holds the if inside a bare block, so the else block's one statement is the block.
func nestedBlock(left bool, right bool) int {
	if left {
		return 1
	} else {
		{
			if right {
				return 2
			}
		}
	}
	return 3
}

// loopInElse holds one loop whose body is one if.
func loopInElse(left bool, values []int) int {
	total := 0
	if left {
		total = 1
	} else {
		for _, value := range values {
			if value > 0 {
				total += value
			}
		}
	}
	return total
}

// switchInElse holds one switch, which an else-if cannot stand for.
func switchInElse(left bool, count int) int {
	if left {
		return 1
	} else {
		switch {
		case count > 0:
			return 2
		}
	}
	return 3
}

// caseClause holds one if as a case's only statement, which is no else.
func caseClause(count int, ready bool) int {
	switch count {
	case 1:
		if ready {
			return 1
		}
	default:
		if !ready {
			return 2
		}
	}
	return 0
}

// functionBody holds one if as the whole body, which is no else.
func functionBody(ready bool) int {
	if ready {
		return 1
	}
	return 0
}

// literalInElse holds one call of a literal whose body is one if, so the if is the literal's and not the else's.
func literalInElse(left bool, right bool) int {
	total := 0
	if left {
		total = 1
	} else {
		func() {
			if right {
				total = 2
			}
		}()
	}
	return total
}
