package parity

import "testing"

// Every group adjudicatedPattern captures is named, so every read of a record takes its group by name (see
// recordGroup), and an alternative added anywhere shifts no other's read. A group with no name could only be read by
// its number, the very read the names replace.
func TestEveryRecordPatternGroupIsNamed(test *testing.T) {
	test.Parallel()
	for index, name := range adjudicatedPattern.SubexpNames() {
		// The first is the whole match, which no group names.
		if index > 0 && name == "" {
			test.Errorf("group %d of adjudicatedPattern has no name, so a read could say what it takes only by its "+
				"number", index)
		}
	}
}
