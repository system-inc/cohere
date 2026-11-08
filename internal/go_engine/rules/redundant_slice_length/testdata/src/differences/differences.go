// Package differences holds the shapes where the port and the original could most plausibly part, and records no
// difference: the two agree on every one.
package differences

type numbers []int

func generic[Slice ~[]int, Text ~string, Pointer ~*[4]int](values Slice, text Text, array Pointer) {
	_ = values[:len(values)]
	_ = text[:len(text)]
	_ = array[:len(array)]
}

func scopes(values []int, rows [][]int) {
	// The inner values is another object than the parameter, and both of its names here are the inner one.
	{
		values := values[1:]
		_ = values[:len(values)]
	}
	for _, row := range rows {
		_ = row[:len(row)]
	}
	_ = func() []int { return values[:len(values)] }
	var named numbers
	_ = named[:len(named)]
	// The low index may be anything, the length itself among it.
	_ = values[len(values):len(values)]
	_ = values[func() int { return 0 }():len(values)]
}

func parameterLength(len func([]int) int, values []int) {
	// A parameter named len shadows the builtin.
	_ = values[:len(values)]
}

func conversions(text string, bytes []byte) {
	// A conversion is no identifier, even where it converts the value len measures.
	_ = []byte(text)[:len(text)]
	_ = string(bytes)[:len(bytes)]
}

func closure(values []int) {
	// The high index is a call, but of a function literal, not of len.
	_ = values[:func() int { return len(values) }()]
}
