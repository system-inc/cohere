package compose

// errorOffset is an error placed at an offset, in UTF-16 code units.
type errorOffset int

// position is getErrorPos for an offset: the one character there.
func (offset errorOffset) position() [2]int {
	return [2]int{int(offset), int(offset) + 1}
}
