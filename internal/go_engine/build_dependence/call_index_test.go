package build_dependence

import (
	"slices"
	"testing"
)

// callingRecord is a decoded package's record of one file whose units each declare one object, named by declares,
// and call the units declaring the objects calls names, by their place in declares.
func callingRecord(identifier string, declares []string, calls map[int][]int) *packageRecord {
	path := "example.com/" + identifier
	record := &packageRecord{identifier: identifier, path: path, imports: map[string]string{}}
	for _, local := range declares {
		record.objects = append(record.objects, recordedObject{path: path, local: local})
	}
	for index, local := range declares {
		unit := recordedUnit{file: identifier + ".go", start: 10 * (index + 1), end: 10*(index+1) + 5, nodes: 3,
			declares: []recordedObject{{path: path, local: local}}}
		for _, callee := range calls[index] {
			unit.callees = append(unit.callees, int32(callee))
		}
		record.units = append(record.units, unit)
	}
	return record
}

// The call index places the standard library's units after the host's, and a call between two of them by the same
// offset (#gf9qrzf): the host's one unit is the index's unit 0, the standard library's Callee, its first unit, is 1,
// and its Caller, which calls Callee, is 2. Caller's row names unit 1 and not the host's unit 0, Callee's lists Caller
// among its callers, and the host's unit calls and is called by nothing.
func TestTheStandardLibrarysOwnCallsLandOnItsOwnUnits(test *testing.T) {
	test.Parallel()

	host := NewGraph()
	host.add(callingRecord("host", []string{"Host"}, nil))
	standardLibrary := NewGraph()
	standardLibrary.add(callingRecord("standard", []string{"Callee", "Caller"}, map[int][]int{1: {0}}))
	index := NewCallIndex(host, standardLibrary)
	if units := index.UnitCount(); units != 3 {
		test.Fatalf("want the host's unit and the standard library's two, got %d units", units)
	}
	for unit, want := range [][2][]int32{{nil, nil}, {nil, {2}}, {{1}, nil}} {
		if calls := index.Calls(unit); !slices.Equal(calls, want[0]) {
			test.Errorf("unit %d calls %v, want %v", unit, calls, want[0])
		}
		if callers := index.Callers(unit); !slices.Equal(callers, want[1]) {
			test.Errorf("unit %d is called by %v, want %v", unit, callers, want[1])
		}
	}
}
