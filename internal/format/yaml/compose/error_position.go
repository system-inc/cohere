package compose

// errorPosition is an error placed at a [start, end] already computed, in UTF-16 code units.
type errorPosition [2]int

// position is getErrorPos for a position: the position itself.
func (sourcePosition errorPosition) position() [2]int {
	return sourcePosition
}
