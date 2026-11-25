package lookalike

// Empty braces that belong to no function body are some other rule's to judge, or nobody's.

type empty struct{}

type handler func()

type closer interface {
	Close()
}

type anything interface{}

func composites() (map[string]int, []int, empty) {
	return map[string]int{}, []int{}, empty{}
}

// A function type in a signature, or a nil function value, has no body.
func apply(callback func(), fallback handler) {
	var unset func()
	if unset == nil {
		callback()
	}
	fallback()
}

// Empty blocks inside a body that does something are statements of that body, not empty functions.
func blocks(ready bool) {
	if ready {
	}
	for range 0 {
	}
	switch {
	}
	{
	}
}

// A bare return is a statement; whether it is redundant is staticcheck's question.
func returns() {
	return
}
