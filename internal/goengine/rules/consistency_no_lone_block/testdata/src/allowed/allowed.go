package allowed

import "strings"

// A bare block around a short variable declaration keeps the variable from leaking into the rest of the function,
// which is what Go uses bare blocks for.
func shortVariable(values []int) int {
	total := 0
	{
		doubled := len(values) * 2
		total += doubled
	}
	return total
}

// A short variable declaration that shadows an outer name is scoped by the braces just the same.
func shadowed(name string) string {
	{
		name := strings.TrimSpace(name)
		if name == "" {
			return "nobody"
		}
	}
	return name
}

// A var declaration binds to the block.
func variable() int {
	{
		var counted int
		counted++
		return counted
	}
}

// A grouped var declaration binds every name in it to the block.
func groupedVariable() int {
	{
		var (
			left  = 1
			right = 2
		)
		return left + right
	}
}

// A const declaration binds to the block.
func constant() int {
	{
		const limit = 3
		return limit
	}
}

// A type declaration binds to the block, a generic one included.
func localType() int {
	{
		type pair[T any] struct{ left, right T }
		return pair[int]{left: 1, right: 2}.right
	}
}

// A declaration after plain statements still makes the braces load-bearing.
func declaresLater(values []int) int {
	total := 0
	{
		total++
		remaining := len(values) - total
		total += remaining
	}
	return total
}

// A whole case wrapped in braces that declare is left alone.
func classify(value int) string {
	switch value {
	case 0:
		{
			kind := "zero"
			return kind
		}
	}
	return "other"
}

// A select case's braces that declare are left alone.
func receive(messages <-chan string) string {
	select {
	case message := <-messages:
		{
			trimmed := strings.TrimSpace(message)
			return trimmed
		}
	default:
		return ""
	}
}

// A labeled bare block is a goto target, so its braces are where a jump lands, not decoration.
func retry(attempt func() bool) int {
	count := 0
again:
	{
		count++
		if !attempt() && count < 3 {
			goto again
		}
	}
	return count
}

// A bare block that declares, inside a function literal, in a generic function, around names in another script.
func firstOf[T any](values []T) func() T {
	return func() T {
		var zero T
		{
			première := zero
			if len(values) > 0 {
				première = values[0]
			}
			return première
		}
	}
}
