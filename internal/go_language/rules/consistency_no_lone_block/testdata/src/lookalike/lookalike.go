package lookalike

// Empty is an empty struct type, whose braces hold fields, not statements.
type Empty struct{}

// Anything is an empty interface type, whose braces hold methods, not statements.
type Anything interface{}

// An empty function body is a body the syntax requires.
func nothing() {}

// Function, if, else, for, range and switch bodies are braces the syntax requires, and none of them declares.
func bodies(values []int) int {
	total := 0
	if len(values) == 0 {
		total = -1
	} else if len(values) == 1 {
		total = values[0]
	} else {
		total = 1
	}
	for index := 0; index < len(values); index++ {
		total += values[index]
	}
	for range values {
		total++
	}
	switch {
	case total > 10:
		total = 10
	}
	return total
}

// A select and a function literal hold braces of their own, and a composite literal's braces hold elements.
func literals(messages <-chan string) []string {
	received := []string{}
	select {
	case message := <-messages:
		received = append(received, message)
	default:
	}
	flush := func() {}
	flush()
	pairs := map[string]struct{}{"one": {}, "two": {}}
	for key := range pairs {
		received = append(received, key)
	}
	return append(received, []string{}...)
}

// A comment and a string that hold braces are a comment and a string.
func text() string {
	// { a comment with braces }
	return "{ a string with braces }"
}
