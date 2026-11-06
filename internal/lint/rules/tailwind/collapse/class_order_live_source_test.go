package tailwind

// classOrderLiveSource is the real Go path: a class name in, a reading out.
//
// This is the seam under test and the reason the whole tier exists. `ParseCandidate` turns the
// string into candidates, the first is taken (which is what `getClassOrder` does at `sort.ts:22`),
// and `Table.Lookup` reads it. Nothing here decodes anything on the fixture's behalf.
type classOrderLiveSource struct {
	name         string
	table        *Table
	designSystem DesignSystem
}

func (source *classOrderLiveSource) Name() string { return source.name }

func (source *classOrderLiveSource) Reading(className string) (Reading, bool) {
	// The variant prefix is stripped before lookup, not before parsing.
	//
	// `md:flex` and `flex` have the same reading, since variants change the selector and the variant
	// bitmask rather than the declarations, and the parser must still see the variants: a class
	// whose variant does not parse is not a class. Parsing the whole string and reading the utility
	// off the parsed candidate is what keeps both true at once.
	candidates := ParseCandidate(className, source.designSystem)
	if len(candidates) == 0 {
		return Reading{}, false
	}
	return source.table.Lookup(&candidates[0])
}
