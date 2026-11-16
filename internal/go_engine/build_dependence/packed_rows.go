package build_dependence

import (
	"fmt"
	"math"
)

// packedRows are rows of entries, one row for each unit, bit or read of a table, laid end to end in one slice: row
// index's entries are entries[offsets[index]:offsets[index+1]]. A slice of slices holds a 24-byte slice header and an
// allocation for every row; packed rows hold a 4-byte offset a row and two allocations in all. An offset is a uint32,
// since a tree's graph holds a few million entries in its largest table, far below 2^32.
type packedRows[Entry int32 | uint32] struct {
	offsets []uint32
	entries []Entry
}

// newPackedRows is a table with no row yet, room made for rows rows and about entries entries, filled by appending to
// its entries and closing each row in turn (closeRow).
func newPackedRows[Entry int32 | uint32](rows int, entries int) packedRows[Entry] {
	return packedRows[Entry]{offsets: make([]uint32, 1, rows+1), entries: make([]Entry, 0, entries)}
}

// closeRow ends the row being filled at the entries appended so far; the next row starts empty.
func (packed *packedRows[Entry]) closeRow() {
	packed.offsets = append(packed.offsets, narrowed[uint32](len(packed.entries), "a table's entries"))
}

// open is the entries of the row being filled, appended since the last row closed.
func (packed *packedRows[Entry]) open() []Entry {
	return packed.entries[packed.offsets[len(packed.offsets)-1]:]
}

// row is row index's entries, read and never written: its capacity ends with it, so an append copies.
func (packed *packedRows[Entry]) row(index int) []Entry {
	return packed.entries[packed.offsets[index]:packed.offsets[index+1]:packed.offsets[index+1]]
}

// rowCount counts the rows closed.
func (packed *packedRows[Entry]) rowCount() int {
	return len(packed.offsets) - 1
}

// rowEntry is one entry of a row of packed rows filled in another order than its rows' (groupedRows).
type rowEntry struct {
	row   int32
	entry int32
}

// groupedRows are pairs as rows rows, each pair's entry in its row, each row's entries in the order of pairs: a count
// of each row's entries, then each entry in its place, so the rows need not be filled in their order.
func groupedRows(rows int, pairs []rowEntry) packedRows[int32] {
	// Every offset is at most the count of pairs, so one check of that count keeps every offset below from wrapping.
	grouped := packedRows[int32]{offsets: make([]uint32, rows+1),
		entries: make([]int32, narrowed[uint32](len(pairs), "a table's entries"))}
	for _, pair := range pairs {
		grouped.offsets[pair.row+1]++
	}
	for index := range rows {
		grouped.offsets[index+1] += grouped.offsets[index]
	}
	next := make([]uint32, rows)
	copy(next, grouped.offsets[:rows])
	for _, pair := range pairs {
		grouped.entries[next[pair.row]] = pair.entry
		next[pair.row]++
	}
	return grouped
}

// column is value as a unit column holds it: a unit's offsets in its file and its size in syntax nodes, which fit 31
// bits in any source file under 2 GiB, thousands of times the largest a build of ours has parsed. A unit past that
// would be read wrong rather than fail, so it fails here instead.
func column(value int) int32 {
	if value < 0 || value > math.MaxInt32 {
		panic("build_dependence: a unit's offset or size passes what a unit column holds")
	}
	return int32(value)
}

// narrowed is value, a count or an index of the graph's units, nodes or entries, as the 32-bit Width that holds it, a
// column's or an offset's. Those are millions in a tree's graph, far below 2^31, but one past it would wrap into
// another index, or into a negative that reads as none, and be read wrong rather than fail, so it fails here instead,
// as column does. counting names what value counts, for the panic.
func narrowed[Width int32 | uint32](value int, counting string) Width {
	narrow := Width(value)
	if value < 0 || int(narrow) != value {
		panic(fmt.Sprintf("build_dependence: %s number %d, more than %T holds", counting, value, narrow))
	}
	return narrow
}
