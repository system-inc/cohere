package mutation_aliasing

import "github.com/system-inc/cohere/static_single_assignment"

// MutableRanges is the table this pass produces: one range per value.
//
// # Why a side table rather than a field on Identifier
//
// Upstream stores the range on the identifier, `identifier.mutableRange`, and oxc does the same in
// its `Environment`. This does not, and the reason was ownership rather than design preference:
// `Identifier` lives in `high_level_intermediate_representation.go`, which was a shared file while
// Stage 1 effect inference was built in this package beside this pass. It holds a second time now
// that the pass is shared: each IR has its own identifier type, and this module holds neither.
//
// The cost is one indirection and one thing a caller must hold. The benefit is that this pass is
// self-contained in one file, which is what makes it reviewable against upstream without reading
// the rest of the package. If ranges become permanent infrastructure, moving them onto `Identifier`
// is a mechanical change and `RangeOf` is the seam that makes it invisible to callers.
//
// A value absent from the table has the unset range, which `Get` returns, so a caller never has to
// distinguish "not computed" from "no range" - upstream does not either, because zero-zero is the
// unset range there.
type MutableRanges struct {
	ranges map[static_single_assignment.IdentifierId]MutableRange
}

// Get returns a value's range, or the unset range when this pass never opened one.
func (ranges *MutableRanges) Get(id static_single_assignment.IdentifierId) MutableRange {
	if ranges == nil || ranges.ranges == nil {
		return MutableRange{}
	}
	return ranges.ranges[id]
}

// Length reports how many values have a range, for measurement.
func (ranges *MutableRanges) Length() int {
	if ranges == nil {
		return 0
	}
	return len(ranges.ranges)
}

// Contains reports whether a value is still being written at an evaluation position.
//
// This is React's `inRange` reached through the table. Half-open, and false for an unset range.
func (ranges *MutableRanges) Contains(
	id static_single_assignment.IdentifierId,
	order static_single_assignment.EvaluationOrder,
) bool {
	return ranges.Get(id).Contains(order)
}

// Set writes a range, creating the table lazily.
func (ranges *MutableRanges) Set(id static_single_assignment.IdentifierId, mutableRange MutableRange) {
	if ranges.ranges == nil {
		ranges.ranges = map[static_single_assignment.IdentifierId]MutableRange{}
	}
	ranges.ranges[id] = mutableRange
}
