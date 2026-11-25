// Package differences holds the shapes where the original and the port could most plausibly part, labels and blocks
// around either statement of the pair, an empty else, a labeled if before the pair, a type parameter's condition and
// blocks of a function literal, and records none, since the two agree on every one.
package differences

// A labeled if is matched through its label, and reported from the label.
func labeledIf(value bool) bool {
	goto check
check:
	if value {
		return true
	}
	return false
}

// A labeled return is matched through its label.
func labeledReturn(value bool) bool {
	switch {
	case !value:
		goto done
	}
	if value {
		return false
	}
done:
	return true
}

// An if alone in a block is matched through the block, and reported from its brace.
func ifInBlock(value bool) bool {
	{
		if value {
			return true
		}
	}
	return false
}

// A return alone in a block is matched through the block.
func returnInBlock(value bool) bool {
	if value {
		return false
	}
	{
		return true
	}
}

// A return alone in blocks inside the body is matched through every one.
func returnInBlocks(value bool) bool {
	if value {
		{
			{
				return true
			}
		}
	}
	return false
}

// A block of two statements around the last return holds no sole return.
func returnInBlockOfTwo(value bool) bool {
	if value {
		return true
	}
	{
		value = !value
		return false
	}
}

// An empty else is the query's nil else.
func emptyElse(value bool) bool {
	if value {
		return true
	} else {
	}
	return false
}

// A labeled if before the pair is no if as the block holds it, so the pair is no series.
func labeledIfBefore(a, b bool) bool {
	goto first
first:
	if a {
		return false
	}
	if b {
		return true
	}
	return false
}

// An if alone in a block before the pair is no if either.
func ifInBlockBefore(a, b bool) bool {
	{
		if a {
			return false
		}
	}
	if b {
		return true
	}
	return false
}

// A labeled pair after a series is still a series by its third to last statement.
func seriesThenLabeled(a, b bool) bool {
	if a {
		goto check
	}
check:
	if b {
		return true
	}
	return false
}

// A condition of a type parameter's type is negated as any identifier is.
func typeParameter[Truth ~bool](value Truth) bool {
	if value {
		return false
	}
	return true
}

// A comparison of a type parameter's values is reversed as any comparison is.
func typeParameterComparison[Number ~int | ~float64](a, b Number) bool {
	if a > b {
		return false
	}
	return true
}

// A builtin length of a type parameter's value is still the builtin.
func typeParameterLength[Text ~string | ~[]byte](text Text) bool {
	if len(text) > 0 {
		return false
	}
	return true
}

// Each block of a function literal, and the block it stands in, is judged on its own.
func literals(value bool) bool {
	check := func() bool {
		if value {
			return true
		}
		return false
	}
	if check() {
		return false
	}
	return true
}

// An if whose condition spans a function literal renders it whole.
func literalInCondition() bool {
	if func() bool {
		return true
	}() {
		return false
	}
	return true
}

// A comment inside the condition's function literal is a comment on the if.
func commentInLiteral() bool {
	if func() bool {
		// Inside the literal.
		return true
	}() {
		return false
	}
	return true
}
