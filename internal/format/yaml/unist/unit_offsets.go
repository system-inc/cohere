package unist

// unitOffsets maps a UTF-16 unit index to its byte offset in the text, the conversion the printer's tree
// loader makes.
type unitOffsets []int

// byteOffset is the unit's byte offset. Past the end of the text, each unit is one byte further.
func (offsets unitOffsets) byteOffset(unit int) int {
	last := len(offsets) - 1
	if unit > last {
		return offsets[last] + unit - last
	}
	if unit < 0 {
		return unit
	}
	return offsets[unit]
}

// convert is the position as Node.Position holds it.
func (offsets unitOffsets) convert(position *position) Position {
	return Position{
		Start: Point{
			Line:   position.start.line,
			Column: position.start.column,
			Offset: offsets.byteOffset(position.start.offset),
		},
		End: Point{
			Line:   position.end.line,
			Column: position.end.column,
			Offset: offsets.byteOffset(position.end.offset),
		},
	}
}
