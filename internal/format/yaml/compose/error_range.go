package compose

// errorRange is an error placed at a node's range, of which the first two numbers are read.
type errorRange []int

// position is getErrorPos for a range: its start and its value end.
func (sourceRange errorRange) position() [2]int {
	return [2]int{sourceRange[0], sourceRange[1]}
}
