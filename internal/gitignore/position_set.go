package gitignore

// positionSet is a bit set over a glob's positions.
type positionSet []uint64

func (positions positionSet) add(position int) { positions[position/64] |= 1 << (position % 64) }

func (positions positionSet) has(position int) bool {
	return positions[position/64]&(1<<(position%64)) != 0
}

func (positions positionSet) clear() { clear(positions) }
