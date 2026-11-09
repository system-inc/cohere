package duplicate_functions

import (
	"encoding/json"
	"testing"
)

// Two lists of parts that write the same text differently never share a digest: every part is tagged and
// length-prefixed. The control is the same list twice, which must agree.
func TestTheBuilderNeverJoinsTwoListsOfParts(test *testing.T) {
	test.Parallel()
	first, second := NewDigestBuilder("a").Text("bc"), NewDigestBuilder("a").Text("bc")
	if first.Sum() != second.Sum() {
		test.Fatalf("one list of parts hashed two ways")
	}
	pairs := []struct {
		name        string
		left, right Digest
	}{
		{"text split differently", NewDigestBuilder("x").Text("ab").Text("c").Sum(),
			NewDigestBuilder("x").Text("a").Text("bc").Sum()},
		{"a number against its text", NewDigestBuilder("x").Number(1).Sum(), NewDigestBuilder("x").Text("1").Sum()},
		{
			"a flag against its opposite",
			NewDigestBuilder("x").Flag(true).Sum(),
			NewDigestBuilder("x").Flag(false).Sum(),
		},
		{"a tag against a part", NewDigestBuilder("xy").Sum(), NewDigestBuilder("x").Text("y").Sum()},
	}
	for _, pair := range pairs {
		if pair.left == pair.right {
			test.Errorf("%s: the two lists share a digest", pair.name)
		}
	}
}

func function(file string, line int, size int, digest Digest, kept string) Function {
	return Function{File: file, Line: line, Name: file, Size: size, Digest: digest, Kept: kept}
}

// A group is two or more functions of one digest at the floor and up, largest first, its size its smallest member's;
// one under the floor is counted and not listed; one whose every member is kept is withheld, while one with a member
// no suppression keeps is listed with its kept member marked. Every member of a group at the floor, listed or
// withheld, is a place a suppression is alive at.
func TestAssembleGroupsByDigestAtTheFloor(test *testing.T) {
	test.Parallel()
	big, small, kept, mixed, lone := Digest{1}, Digest{2}, Digest{3}, Digest{4}, Digest{5}
	functions := []Function{
		function("b.ts", 3, 21, big, ""), function("a.ts", 9, 20, big, ""),
		function("c.ts", 1, 10, small, ""), function("d.ts", 1, 10, small, ""),
		function("e.ts", 1, 30, kept, "port of upstream"), function("f.ts", 1, 30, kept, "port of upstream"),
		function("g.ts", 1, Floor, mixed, "port of upstream"), function("h.ts", 1, Floor, mixed, ""),
		function("h.ts", 5, Floor, mixed, ""), function("i.ts", 1, 40, lone, ""),
	}
	report := &Report{}
	atFloor := report.Assemble(functions)
	if report.Floor != Floor || report.Hashed != len(functions) || report.Functions != len(functions) {
		test.Fatalf("the floor, hashed and functions read %d, %d, %d", report.Floor, report.Hashed, report.Functions)
	}
	if len(report.Groups) != 2 || report.Withheld != 1 || report.BelowFloor != 1 {
		test.Fatalf("assembled %d groups, %d withheld, %d under the floor, where 2, 1 and 1 were expected: %+v",
			len(report.Groups), report.Withheld, report.BelowFloor, report.Groups)
	}
	first, second := report.Groups[0], report.Groups[1]
	if first.Digest != big || first.Size != 20 || first.Members[0].File != "a.ts" {
		test.Errorf("the first group is %+v, where the size-20 group led by a.ts was expected", first)
	}
	if second.Digest != mixed || len(second.Members) != 3 || second.Members[0].Kept != "port of upstream" {
		test.Errorf("the second group is %+v, where the three members with g.ts kept were expected", second)
	}
	for _, place := range []Place{{"e.ts", 1}, {"g.ts", 1}, {"a.ts", 9}} {
		if !atFloor[place] {
			test.Errorf("%v is a member of a group at the floor and was not returned as one", place)
		}
	}
	for _, place := range []Place{{"c.ts", 1}, {"i.ts", 1}} {
		if atFloor[place] {
			test.Errorf("%v is in no group at the floor and was returned as one", place)
		}
	}
}

// The digest travels as hex, and anything that is not sixteen bytes of it is refused rather than read as zeros.
func TestADigestTravelsAsHex(test *testing.T) {
	test.Parallel()
	digest := NewDigestBuilder("x").Sum()
	encoded, encodeError := json.Marshal(digest)
	if encodeError != nil {
		test.Fatalf("encoding: %v", encodeError)
	}
	var decoded Digest
	if decodeError := json.Unmarshal(encoded, &decoded); decodeError != nil || decoded != digest {
		test.Fatalf("%s read back as %v (%v)", encoded, decoded, decodeError)
	}
	for _, bad := range []string{`"abc"`, `"zz"`, `"00"`} {
		if json.Unmarshal([]byte(bad), &decoded) == nil {
			test.Errorf("%s read as a digest", bad)
		}
	}
}
