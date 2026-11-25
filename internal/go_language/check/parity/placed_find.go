package parity

import (
	"cmp"
	"fmt"
	"slices"
	"testing"
)

// placedFind is a `finds at column` record: its finding as the record places it, by file, line, column and message,
// and, where holdsEnd, by its end too, EndLine and EndColumn, both 0 for a record that says it has no end.
type placedFind struct {
	at       Finding
	holdsEnd bool
}

// placement is where record says its finding sits, as a failure says it: "starts at column 2", and, where it holds
// the end, "and ends at line 9, column 10" or "and has no end".
func (record placedFind) placement() string {
	placement := fmt.Sprintf("starts at column %d", record.at.Column)
	switch {
	case !record.holdsEnd:
		return placement
	case record.at.EndLine == 0:
		return placement + " and has no end"
	default:
		return fmt.Sprintf("%s and ends at line %d, column %d", placement, record.at.EndLine, record.at.EndColumn)
	}
}

// isMetBy is whether finding sits where record places it: at its file, line, column and message, and at its end where
// it holds one.
func (record placedFind) isMetBy(finding Finding) bool {
	if finding.File != record.at.File || finding.Line != record.at.Line || finding.Column != record.at.Column ||
		finding.Message != record.at.Message {
		return false
	}
	return !record.holdsEnd || (finding.EndLine == record.at.EndLine && finding.EndColumn == record.at.EndColumn)
}

// requirePlaced fails the test for each record of placed, a `finds at column` record, that no finding of found meets
// at its file, line, column and message, and at its end where it holds one, saying that the fixture records what
// finds, as "the port alone finds". Each finding meets one record, those that hold an end met first, so one that holds
// none never takes the finding another needs. The record's line and message are matched as any `finds:` record's
// are, so this holds where along the line it sits alone.
func requirePlaced(test testing.TB, finds string, placed []placedFind, found []Finding) {
	test.Helper()
	ordered := slices.Clone(placed)
	slices.SortStableFunc(ordered, func(first placedFind, second placedFind) int {
		return cmp.Compare(boolOrder(second.holdsEnd), boolOrder(first.holdsEnd))
	})
	taken := make([]bool, len(found))
	for _, record := range ordered {
		met := false
		for index, finding := range found {
			if taken[index] || !record.isMetBy(finding) {
				continue
			}
			taken[index], met = true, true
			break
		}
		if !met {
			test.Errorf("the fixture records that %s %s, and no finding of the port's there %s", finds, record.at,
				record.placement())
		}
	}
}

// boolOrder is 1 for true and 0 for false, so a sort can order by it.
func boolOrder(value bool) int {
	if value {
		return 1
	}
	return 0
}
