package allowed

import "fmt"

type node struct{ name string }

func use(value any) {}

// The comma-ok definition cannot panic: a miss sets ok to false.
func definition(value any) {
	typed, ok := value.(*node)
	if !ok {
		return
	}
	use(typed)
}

// So can the comma-ok in an if statement's init, the shape most checks take.
func guarded(value any) string {
	if typed, ok := value.(fmt.Stringer); ok {
		return typed.String()
	}
	return ""
}

// A comma-ok assignment, a comma-ok var declaration, and either one through parentheses.
func forms(value any) {
	var typed *node
	var ok bool
	typed, ok = value.(*node)
	var counted, isInteger = value.(int)
	parenthesized, isNode := (value.(*node))
	use(typed)
	use(ok)
	use(counted)
	use(isInteger)
	use(parenthesized)
	use(isNode)
}

// Dropping ok still cannot panic, so the rule leaves it: what the miss means is a question for the reader, not a crash.
func discarded(value any) {
	typed, _ := value.(*node)
	_, _ = value.(int)
	use(typed)
}

// A package-level comma-ok declaration.
var initialized any = "text"

var asserted, isString = initialized.(string)

// A type switch's value.(type) is no assertion: a value no case matches runs the default case.
func switched(value any) string {
	switch typed := value.(type) {
	case *node:
		return typed.name
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

// A type switch with no binding is the same.
func switchedBare(value any) bool {
	switch value.(type) {
	case int, string:
		return true
	}
	return false
}

// A type parameter's value, checked through any.
func generic[Element any](value any) (Element, bool) {
	typed, ok := value.(Element)
	return typed, ok
}
