package differences

type holder struct {
	value interface{}
}

// Shapes where the two could most plausibly part, where both agree.
func agreed(i interface{}, x interface{}, record holder, values []interface{}) {
	// An else holding only a comment is an else of no statements, on either if.
	if i != nil {
		if _, ok := i.(string); ok {
		} else {
			// nothing
		}
	} else {
		// nothing
	}

	// Labels and blocks around the inner if at more than one depth.
	if i != nil {
	outer:
		{
		inner:
			{
				if _, ok := i.(string); ok {
					goto outer
					goto inner
				}
			}
		}
	}
	if i != nil {
	first:
	second:
		if _, ok := i.(string); ok {
			goto first
			goto second
		}
	}

	// A labeled block of two statements holds no sole if.
	if i != nil {
	label:
		{
			if _, ok := i.(string); ok {
				goto label
			}
			println()
		}
	}

	// The first query inside the shape of the second is reported once, by the first.
	if i != nil {
		if _, ok := i.(string); ok && i != nil {
		}
	}

	// An else if whose own if matches is judged as an if of its own.
	if x != nil {
	} else if _, ok := i.(string); ok && i != nil {
	}

	// The inner if of a nested shape that is itself an outer if of the second query.
	if i != nil {
		if x != nil {
			if _, ok := x.(string); ok {
			}
		}
	}

	// A function literal's ifs are judged as any other.
	check := func(value interface{}) {
		if value != nil {
			if _, ok := value.(error); ok {
			}
		}
		if _, ok := value.(error); value != nil && ok {
		}
	}
	check(i)

	// The asserted value is an index or a selector, which no `Object` binds.
	if _, ok := values[0].(string); ok && values[0] != nil {
	}
	if record.value != nil {
		if _, ok := record.value.(string); ok {
		}
	}

	// A variable shadowing the parameter inside the outer body is another object.
	if i != nil {
		if i, ok := x.(string); ok {
			_ = i
		}
	}
}
