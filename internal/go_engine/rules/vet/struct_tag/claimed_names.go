package struct_tag

import "go/token"

// claimedNames is the names one struct's fields claim, each at the position of the field that claimed it first.
// x/tools' namesSeen.
type claimedNames map[encodedName]token.Pos

// claim records name at position, and is false, when no field claimed it before, and otherwise is the position of
// the field that did, and true, leaving that claim as it was. x/tools' namesSeen Get, then Set when Get finds
// nothing, as its checkTagDuplicates calls them.
func (names claimedNames) claim(name encodedName, position token.Pos) (token.Pos, bool) {
	claimedAt, isClaimed := names[name]
	if !isClaimed {
		names[name] = position
	}
	return claimedAt, isClaimed
}
