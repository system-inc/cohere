package build_dependence

// fileTable is every file of the graph's packages, by its package's ID and the file a reader sees, each a row: its
// name, and its units in the graph's order, which is each file's start order, since a package's units come in the
// order of its files' declarations (collector.collectFile) and a record keeps that order. A file's units are
// contiguous unless a //line directive names two files in one file parsed, a generator's output say, or two files
// parsed name one file; then unitOrder holds every row's units, the graph's units grouped by file. Otherwise it is
// nil, row r's units being the graph's units unitOffsets[r] up to unitOffsets[r+1], and costs nothing.
type fileTable struct {
	rows        map[fileKey]int32
	names       []string
	unitOffsets []uint32
	unitOrder   []int32

	// inStartOrder says each file's units are in the order of their start offsets, as they always are but for two
	// files parsed naming one, which UnitDependent then finds a unit of by reading each of the file's units in turn.
	inStartOrder bool

	// overlapping marks each row with a unit that starts before a unit ahead of it in the row ends, out of start order
	// or inside its span, as when two files parsed name one file: the cgo tool's generated file names the cgo file its
	// translation names, past the `//line` it writes inside an exported function's wrapper (cmd/cgo's writeExports).
	// No search by start finds the unit holding an offset in such a row, so unitContaining reads each of its units.
	// Nil when no row is.
	overlapping []bool
}

// row is the row of the file key names, made if it has none.
func (files *fileTable) row(key fileKey) int32 {
	if row, isFile := files.rows[key]; isFile {
		return row
	}
	row := int32(len(files.names))
	files.rows[key] = row
	files.names = append(files.names, key.file)
	return row
}

// placeUnits finds each row's units, from each unit's row, start and end offsets: their range, their order when a
// file's units are apart (fileTable), and the rows whose spans overlap (fileTable.overlapping). Rows are numbered in
// the order their first units come, so a file's units are contiguous, every file's, exactly when no unit's row is
// below the row of the unit before it.
func (files *fileTable) placeUnits(unitFile []int32, unitStart []int32, unitEnd []int32) {
	rows := len(files.names)
	files.unitOffsets = make([]uint32, rows+1)
	contiguous := true
	for unit, row := range unitFile {
		files.unitOffsets[row+1]++
		if unit > 0 && row < unitFile[unit-1] {
			contiguous = false
		}
	}
	for row := range rows {
		files.unitOffsets[row+1] += files.unitOffsets[row]
	}
	files.inStartOrder = true
	lastStart := make([]int32, rows)
	for row := range lastStart {
		lastStart[row] = -1
	}
	// The furthest end of each row's units so far, so a unit starting before it overlaps one ahead of it.
	furthestEnd := make([]int32, rows)
	for unit, row := range unitFile {
		if unitStart[unit] < lastStart[row] {
			files.inStartOrder = false
		}
		if unitStart[unit] < furthestEnd[row] {
			if files.overlapping == nil {
				files.overlapping = make([]bool, rows)
			}
			files.overlapping[row] = true
		}
		lastStart[row] = unitStart[unit]
		furthestEnd[row] = max(furthestEnd[row], unitEnd[unit])
	}
	if contiguous {
		return
	}
	files.unitOrder = make([]int32, len(unitFile))
	next := make([]uint32, rows)
	copy(next, files.unitOffsets[:rows])
	for unit, row := range unitFile {
		files.unitOrder[next[row]] = int32(unit)
		next[row]++
	}
}

// unitAt is the unit at position in the file table's order: the graph's own, unless a file's units are apart.
func (files *fileTable) unitAt(position uint32) int32 {
	if files.unitOrder == nil {
		return int32(position)
	}
	return files.unitOrder[position]
}
