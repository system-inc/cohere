package parity

import (
	"parity/other"
	. "parity/other"
)

type holder struct {
	ok    bool
	value interface{}
}

func produce() (interface{}, bool) { return nil, false }

// The first query: the initializer asserts, and the condition joins ok and the asserted variable's nil check.
func joinedReported(i interface{}, x interface{}) {
	if _, ok := i.(string); ok && i != nil {
	}
	if _, ok := i.(string); i != nil && ok {
	}
	if _, matched := x.(error); matched && x != nil {
	}

	// An assignment by `=` is one, as the pattern's token is `_`.
	var ok bool
	if _, ok = i.(int); ok && i != nil {
	}
	if _, (ok) = i.(int); i != nil && ok {
	}

	// The pattern sees through parentheses around every operand and the whole condition.
	if _, ok := (i).(string); (ok) && (i) != (nil) {
	}
	if _, ok := i.(string); ok && (i != nil) {
	}
	if _, ok := i.(string); (i != nil) && (ok) {
	}

	// The body and the else are anything.
	if _, ok := i.(string); ok && i != nil {
		println()
	} else {
		println()
	}
	if _, ok := i.(string); ok && i != nil {
	} else if x != nil {
	}

	// A package's variable named through a dot import and through its package name is one object.
	if _, ok := Value.(string); ok && other.Value != nil {
	}
	if _, Flag = i.(string); other.Flag && i != nil {
	}
}

func joinedLeftAlone(i interface{}, x interface{}, record holder, values map[string]int) {
	// The condition is no `&&` of ok and the nil check, in either order.
	if _, ok := i.(string); i != nil || ok {
	}
	if _, ok := i.(string); i != nil && !ok {
	}
	if _, ok := i.(string); i == nil && ok {
	}
	if _, ok := i.(string); nil != i && ok {
	}
	if _, ok := i.(string); ok && nil != i {
	}
	if _, ok := i.(string); ok {
	}
	if _, ok := i.(string); ok && i != nil && x != nil {
	}
	if _, ok := i.(string); ok && x != nil {
	}

	// The left of the assignment is not the blank identifier and ok.
	if text, ok := i.(string); ok && i != nil {
		_ = text
	}
	if _, record.ok = i.(string); record.ok && i != nil {
	}

	// The right of the assignment is no type assertion.
	if _, ok := values["key"]; ok && i != nil {
	}
	if _, ok := produce(); ok && i != nil {
	}

	// The asserted value is no identifier.
	if _, ok := record.value.(string); ok && record.value != nil {
	}
	if _, ok := other.Value.(string); ok && Value != nil {
	}

	// ok in the condition is another object: a field spelled alike.
	if _, ok := i.(string); record.ok && i != nil {
		_ = ok
	}

	// A local variable that shadows the dot-imported one is another object than the package's.
	{
		Value := x
		if _, ok := Value.(string); ok && other.Value != nil {
		}
	}

	// No initializer, or one that is no assignment.
	ok := true
	if ok && i != nil {
	}
	if i != nil && ok {
	}
}

// The second query: an if of no initializer and no else checks a variable against nil around a sole if that asserts
// on it and tests ok alone.
func nestedReported(i interface{}, x interface{}) {
	if i != nil {
		if _, ok := i.(string); ok {
		}
	}
	if x != nil {
		if _, matched := x.(error); matched {
			println()
		}
	}

	// An assignment by `=` is one.
	var ok bool
	if i != nil {
		if _, ok = i.(int); ok {
		}
	}

	// Parentheses are seen through.
	if (i) != (nil) {
		if _, ok := (i).(string); ok {
		}
	}

	// An else of no statements is none, on either if.
	if i != nil {
		if _, ok := i.(string); ok {
		} else {
		}
	}
	if i != nil {
		if _, ok := i.(string); ok {
		}
	} else {
	}

	// The inner if is seen through a label and through a block of one statement.
	if i != nil {
	label:
		if _, ok := i.(string); ok {
			goto label
		}
	}
	if i != nil {
		{
			if _, ok := i.(string); ok {
			}
		}
	}

	// The inner if recalls the outer's object, which a selector through the package name names too.
	if Value != nil {
		if _, ok := other.Value.(string); ok {
		}
	}

	// Ifs nested three deep are reported once, at the innermost, whose parent alone matches.
	if i != nil {
		if i != nil {
			if _, ok := i.(string); ok {
			}
		}
	}
}

func nestedLeftAlone(i interface{}, x interface{}, record holder) {
	// The outer if has an initializer.
	if y := i; y != nil {
		if _, ok := y.(string); ok {
		}
	}

	// The outer if has an else of statements, or an else if.
	if i != nil {
		if _, ok := i.(string); ok {
		}
	} else {
		println()
	}
	if i != nil {
		if _, ok := i.(string); ok {
		}
	} else if x != nil {
	}

	// The outer body is more than the inner if.
	if i != nil {
		if _, ok := i.(string); ok {
		}
		println(i)
	}
	if i != nil {
		{
			if _, ok := i.(string); ok {
			}
			println()
		}
	}

	// The outer condition is no `!=` of an identifier with nil.
	if i == nil {
		if _, ok := i.(string); ok {
		}
	}
	if nil != i {
		if _, ok := i.(string); ok {
		}
	}
	if record.value != nil {
		if _, ok := record.value.(string); ok {
		}
	}
	if other.Value != nil {
		if _, ok := Value.(string); ok {
		}
	}
	if i != nil && x != nil {
		if _, ok := i.(string); ok {
		}
	}

	// The inner if has an else of statements, or an else if, or an else holding an empty block.
	if i != nil {
		if _, ok := i.(string); ok {
		} else {
			println()
		}
	}
	if i != nil {
		if _, ok := i.(string); ok {
		} else if x != nil {
		}
	}
	if i != nil {
		if _, ok := i.(string); ok {
		} else {
			{
			}
		}
	}

	// The inner condition is no bare ok.
	if i != nil {
		if _, ok := i.(string); !ok {
		}
	}
	if i != nil {
		if _, ok := i.(string); ok && x != nil {
		}
	}
	if i != nil {
		if _, ok := i.(string); record.ok {
			_ = ok
		}
	}

	// The inner if asserts on another variable, or into no blank identifier.
	if x != nil {
		if _, ok := i.(string); ok {
		}
	}
	if i != nil {
		if text, ok := i.(string); ok {
			_ = text
		}
	}

	// The outer body is no if.
	if i != nil {
		for _, ok := i.(string); ok; {
		}
	}
}

// A nil the function declares is not the universe's, so neither query matches it.
func shadowedNil(i interface{}) {
	nil := (*int)(nil)
	if _, ok := i.(string); ok && i != nil {
	}
	if i != nil {
		if _, ok := i.(string); ok {
		}
	}
}
