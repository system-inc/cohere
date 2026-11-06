package flagged

import "errors"

// A labeled block that a goto inside it leaves forward, and one inside it goes back to.
func validate(names []string) error {
	var validateError error
	attempt := 0
check: // want "`check` is a label"
	{
		attempt++
		if attempt > len(names) {
			validateError = errors.New("naming nothing")
			goto done
		}
		if names[attempt-1] == "" {
			goto check
		}
	}
done: // want "`done` is a label"
	return validateError
}

// A goto that lands on an empty statement closing the function, and one that goes back to an if.
func countDown(start int, step func()) {
again: // want "`again` is a label"
	if start > 0 {
		start--
		step()
		goto again
	}
	if start < 0 {
		goto end
	}
	step()
end: // want "`end` is a label"
}

// A label inside a function literal is flagged where it stands, in the literal.
var firstEven = func(values []int) int {
	even := -1
search: // want "`search` is a label"
	for _, value := range values {
		if value%2 == 0 {
			even = value
			break search
		}
	}
	return even
}

// A label inside a generic function, and a name in another script, are labels like any other.
func firstMatch[T comparable](values []T, target T) int {
	index := -1
découverte: // want "`découverte` is a label"
	for position, value := range values {
		if value == target {
			index = position
			break découverte
		}
	}
	return index
}
