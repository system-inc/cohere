// Package none imports nothing, so the index holds no package, not even its own, whose field and method it uses.
package none

const Value = 1

func doubled(value int) int {
	return value * 2
}

type pair struct {
	first int
}

func (value pair) sum() int {
	return value.first
}

func summed() int {
	return pair{first: 1}.sum()
}
