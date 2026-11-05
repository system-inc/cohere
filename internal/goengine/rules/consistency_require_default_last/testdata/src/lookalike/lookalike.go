package lookalike

// Default is a name, not the keyword, and a case that matches it is an ordinary case.
const Default = 3

// caseNamedDefault has a case matching a constant named Default before its last clause.
func caseNamedDefault(count int) string {
	switch count {
	case Default:
		return "the default count"
	case 1:
		return "one"
	}
	return "other"
}

// fallsOutOfDefault runs the default and then the case after it, so moving the default last would change what runs.
func fallsOutOfDefault(count int) int {
	switch count {
	default:
		count = 0
		fallthrough
	case 1:
		count++
	}
	return count
}

// fallsIntoDefault runs the case before the default and then the default, so moving it would change what runs.
func fallsIntoDefault(count int) int {
	switch count {
	case 0:
		count++
		fallthrough
	default:
		count *= 2
	case 1:
		count--
	}
	return count
}

// fallsOutPastSemicolon ends its default on fallthrough and two semicolons, the second of which leaves an empty
// statement after the fallthrough that gofmt would remove.
func fallsOutPastSemicolon(count int) int {
	switch count {
	default:
		count = 0
		fallthrough;;
	case 1:
		count++
	}
	return count
}

// defaultInComment mentions default: first in a comment above a switch that has none.
func defaultInComment(count int) string {
	// default: would go here, before case 0, in a switch the house does not write.
	switch count {
	case 0:
		return "none"
	}
	return "default:"
}

// selectWithDefaultMessage has a select whose cases receive values named for a default, with no default clause.
func selectWithDefaultMessage(defaults chan string, done chan struct{}) string {
	select {
	case defaultValue := <-defaults:
		return defaultValue
	case <-done:
		return "default"
	}
}
