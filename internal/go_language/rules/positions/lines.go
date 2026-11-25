package positions

import "go/token"

// Lines is the table's view of one file, for a caller that asks the line of many positions in that file, mostly in
// the order they come: each is looked up among that file's lines alone, first on the line the caller expects.
type Lines struct {
	table *Table
	// held is the file the view reads, or nil when the table does not hold it.
	held *fileLines
}

// LineNear is what table.Line(position) is, position's line in its file as written or 0 for token.NoPos, read first
// on line near. That costs two comparisons when near is position's line, as it mostly is for a walk that asks of
// positions in the order they come in the file, or of a node's end after its start. A position not on line near, or a
// near that is no line of the file, is searched for among the file's lines, and a position outside the view's file,
// past its end or in another, is read from the table.
func (lines Lines) LineNear(position token.Pos, near int) int {
	at := int(position)
	if lines.held == nil || at < lines.held.base || at > lines.held.base+lines.held.size {
		return lines.table.Line(position)
	}
	offset := at - lines.held.base
	// Line offsets only grow, so a position at or after near's start and before the next line's, if there is one,
	// is on near and on no other line.
	if near >= 1 && near <= len(lines.held.lineOffsets) && lines.held.lineOffsets[near-1] <= offset &&
		(near == len(lines.held.lineOffsets) || offset < lines.held.lineOffsets[near]) {
		return near
	}
	return lineOf(lines.held.lineOffsets, offset)
}

// LineCount is how many lines the view's file has, or 0 when the table does not hold the file, so has no lines of it
// to count.
func (lines Lines) LineCount() int {
	if lines.held == nil {
		return 0
	}
	return len(lines.held.lineOffsets)
}
