package flagged

import "os"

// An empty bare block scopes nothing.
func empty() {
	{ // want "These braces do nothing"
	}
}

// A bare block around plain statements groups them for the eye and for nothing else.
func plain(values []int) int {
	total := 0
	{ // want "These braces do nothing. A bare block scopes only what is declared directly inside it"
		total += len(values)
		total *= 2
	}
	return total
}

// A defer inside a bare block still waits for the function to return, so the braces mislead about when it runs.
func deferred(path string) error {
	file, openError := os.Open(path)
	if openError != nil {
		return openError
	}
	{ // want "These braces do nothing"
		defer file.Close()
	}
	return nil
}

// A short variable declaration in an if's init belongs to the if, not to the block around it.
func initOnly(values map[string]int) int {
	total := 0
	{ // want "These braces do nothing"
		if value, found := values["total"]; found {
			total = value
		}
	}
	return total
}

// A for's clause and a range's variables belong to the loop, not to the block around it.
func loopOnly(values []int) int {
	total := 0
	{ // want "These braces do nothing"
		for index := 0; index < len(values); index++ {
			total += values[index]
		}
		for _, value := range values {
			total += value
		}
	}
	return total
}

// A declaration inside a nested block or a function literal belongs to that, so the outer braces scope nothing and
// the inner block, which declares, is left alone.
func nested() int {
	total := 0
	{ // want "These braces do nothing"
		{
			inner := 2
			total += inner
		}
		func() {
			step := 1
			total += step
		}()
	}
	return total
}

// Blocks stacked on blocks are each judged: here every one of them declares nothing.
func stacked() int {
	total := 0
	{ // want "These braces do nothing"
		{ // want "These braces do nothing"
			total++
		}
	}
	return total
}

// A switch's case is already a scope, so braces wrapping a whole case that declare nothing add nothing.
func classify(value int) string {
	kind := "other"
	switch value {
	case 0:
		{ // want "These braces do nothing"
			kind = "zero"
		}
	default:
		kind = "nonzero"
		{ // want "These braces do nothing"
		}
	}
	return kind
}

// A type switch's case is judged the same way.
func describe(value any) string {
	kind := "other"
	switch value.(type) {
	case string:
		{ // want "These braces do nothing"
			kind = "string"
		}
	}
	return kind
}

// A select's case holds a statement list too.
func receive(messages <-chan string) string {
	received := ""
	select {
	case message := <-messages:
		{ // want "These braces do nothing"
			received = message
		}
	default:
	}
	return received
}

// A bare block inside a function literal, and one assigning with = rather than declaring with :=.
var reset = func(values []int) {
	{ // want "These braces do nothing"
		values[0] = 0
		values[0], values[1] = values[1], values[0]
	}
}

// A bare block inside a generic function, around names in another script.
func firstOf[T any](values []T) T {
	var première T
	{ // want "These braces do nothing"
		if len(values) > 0 {
			première = values[0]
		}
	}
	return première
}

// A block that declares something sits inside one that declares nothing, which is still reported.
func outerOnly() int {
	{ // want "These braces do nothing"
		{
			var counted int
			counted++
			return counted
		}
	}
}
