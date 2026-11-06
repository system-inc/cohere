package flagged

// expressionFirst opens its switch on the default, so a reader meets the fallback before the cases it stands behind.
func expressionFirst(count int) string {
	switch count {
	default: // want `not the last clause of its switch`
		return "many"
	case 0:
		return "none"
	case 1:
		return "one"
	}
}

// expressionMiddle puts the default between two cases.
func expressionMiddle(count int) string {
	switch {
	case count < 0:
		return "negative"
	default: // want `not the last clause of its switch`
		return "some"
	case count == 0:
		return "none"
	}
}

// emptyDefault has nothing in its default, and its place still misleads the reader.
func emptyDefault(count int) int {
	switch count {
	default: // want `not the last clause of its switch`
	case 1:
		count++
	}
	return count
}

// typeFirst opens its type switch on the default.
func typeFirst(value any) string {
	switch typed := value.(type) {
	default: // want `not the last clause of its type switch`
		return "unknown"
	case string:
		return typed
	}
}

// typeMiddle puts the default between two type cases, with no binding.
func typeMiddle(value any) string {
	switch value.(type) {
	case int:
		return "int"
	default: // want `not the last clause of its type switch`
		return "unknown"
	case string:
		return "string"
	}
}

// selectFirst opens its select on the default.
func selectFirst(ready chan int) int {
	select {
	default: // want `not the last clause of its select`
		return 0
	case value := <-ready:
		return value
	}
}

// selectMiddle puts the default between a receive and a send.
func selectMiddle(ready chan int, done chan struct{}) int {
	select {
	case value := <-ready:
		return value
	default: // want `not the last clause of its select`
		return 0
	case done <- struct{}{}:
		return 1
	}
}

// nested holds a switch whose default is last around one whose default is first, and only the inner one is flagged.
func nested(count int, value any) string {
	switch count {
	case 0:
		switch value.(type) {
		default: // want `not the last clause of its type switch`
			return "zero of something"
		case int:
			return "zero int"
		}
	default:
		return "other"
	}
}

// insideClosure holds the switch in a function literal, which is walked like any other body.
var insideClosure = func(count int) string {
	switch count {
	default: // want `not the last clause of its switch`
		return "many"
	case 1:
		return "one"
	}
}

// fallthroughElsewhere has a fallthrough between two other cases, which does not tie the default to anything.
func fallthroughElsewhere(count int) int {
	switch count {
	case 0:
		count++
		fallthrough
	case 1:
		count++
	default: // want `not the last clause of its switch`
		count = 0
	case 2:
		count--
	}
	return count
}

// nestedFallthrough has a fallthrough inside a nested switch in the clause before the default, which belongs to the
// inner switch and does not reach this default.
func nestedFallthrough(count int, other int) int {
	switch count {
	case 0:
		switch other {
		case 0:
			fallthrough
		case 1:
			count++
		}
	default: // want `not the last clause of its switch`
		count = 0
	case 2:
		count--
	}
	return count
}
