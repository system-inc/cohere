package parity

import (
	"parity/other"
	. "parity/other"
)

type holder struct {
	values []int
}

func produce() []int { return nil }

func size(values []int) int { return len(values) }

func reported(values []int, text string, array [4]int, pointer *[4]int, index int) {
	_ = values[:len(values)]
	_ = values[1:len(values)]
	_ = values[index:len(values)]
	_ = text[:len(text)]
	_ = text[index:len(text)]
	_ = array[:len(array)]
	_ = pointer[:len(pointer)]

	// The pattern sees through parentheses around the sliced value, the high index, len and its argument, and the
	// finding spans the high index with its parentheses.
	_ = (values)[:len(values)]
	_ = (values)[index:len((values))]
	_ = values[:(len(values))]
	_ = values[:(len)(values)]
	_ = values[:len((values))]

	// A package's variable named through a dot import and through its package name is one object.
	_ = Values[:len(other.Values)]
	_ = Values[:len(Values)]
	_ = Text[1:len(other.Text)]

	const constant = "constant"
	_ = constant[:len(constant)]
}

func leftAlone(values []int, others []int, text string, nested [][]int, record holder, index int) {
	// A three-index slice sets the capacity, so its high index can't be left out.
	_ = values[:len(values):len(values)]
	_ = values[index:len(values):cap(values)]

	// The high index measures another value.
	_ = values[:len(others)]
	_ = values[:len(text)]

	// The high index is no call to len of the value alone.
	_ = values[:]
	_ = values[index:]
	_ = values[:cap(values)]
	_ = values[:len(values)-1]
	_ = values[:size(values)]
	_ = values[:index]

	// The sliced value is no identifier: a selector, an index or a call.
	_ = record.values[:len(record.values)]
	_ = nested[0][:len(nested[0])]
	_ = produce()[:len(produce())]
	_ = other.Values[:len(other.Values)]

	// A local variable that shadows the dot-imported one is another object than the package's.
	{
		Values := others
		_ = Values[:len(other.Values)]
	}
}

func shadowedLength(values []int) {
	len := func(values []int) int { return 1 }
	_ = values[:len(values)]
}
