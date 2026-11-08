// Package parity holds the comparisons SA4022 judges, flagged and left alone, for the port and the original to judge
// side by side: the address of every kind of operand compared with nil by == and by !=, parentheses anywhere the
// pattern looks through them, and the comparisons it refuses for their order, their operator or a nil that is no
// builtin.
package parity

type point struct {
	x int
	y *int
}

var global int

func flagged(value int, pointer *int, location point, values []int, array [3]int, table map[string]*int) bool {
	_ = &value == nil
	_ = &value != nil
	_ = &pointer == nil
	_ = &global != nil
	_ = &location.x == nil
	_ = &location.y != nil
	_ = &values[0] == nil
	_ = &array[1] != nil
	_ = &point{} == nil
	_ = &[]int{1} != nil
	_ = (&value) == nil
	_ = &(value) == nil
	_ = &value == (nil)
	_ = ((&value) != (nil))
	_ = (&value == nil) == true
	_ = &value == nil && &pointer != nil
	_ = func() bool { return &value == nil }
	if &value != nil {
		return true
	}
	for &value == nil {
	}
	return &location == nil
}

func notFlagged(value int, pointer *int, other *int) {
	_ = nil == &value
	_ = nil != &value
	_ = pointer == nil
	_ = pointer != nil
	_ = &value == other
	_ = &value == pointer
	_ = &value == (*int)(nil)
	_ = *&pointer == nil
	_ = &value == &value
	{
		nil := other
		_ = &value == nil
		_ = &value != (nil)
	}
}

func generic[Element any](element Element, elements []Element) bool {
	_ = &element == nil
	_ = &elements[0] != nil
	return &element != nil
}

func instantiate() {
	_ = generic(1, []int{1})
	_ = generic[string]("one", nil)
}
