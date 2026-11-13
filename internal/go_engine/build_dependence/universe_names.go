package build_dependence

import (
	"go/types"
	"slices"
)

// universeNameBits are the universe's names, each with its bit in a unit's universeNames, in the universe's order.
var universeNameBits = universeNameIndex()

// universeNameIndex numbers the universe's names. Go 1.27's universe holds 44, so each has a bit of its own; were it
// ever to hold more than 64, the names past the 63rd would share the last bit, which seeds more units than it needs to
// and never fewer.
func universeNameIndex() map[string]uint64 {
	names := types.Universe.Names()
	slices.Sort(names)
	index := make(map[string]uint64, len(names))
	for position, name := range names {
		index[name] = uint64(1) << min(position, 63)
	}
	return index
}

// universeNameBit is name's bit, zero for a name the universe does not hold.
func universeNameBit(name string) uint64 {
	return universeNameBits[name]
}
