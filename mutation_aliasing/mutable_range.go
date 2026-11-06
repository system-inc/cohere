package mutation_aliasing

import "github.com/system-inc/cohere/static_single_assignment"

// MutableRange is the half-open span of evaluation over which a value is still being written.
//
// Membership is `Start <= order < End`, which is React's `inRange` exactly. A range with both
// fields zero is unset: the value has no recorded definition point. That is a real state rather
// than an error - a function's parameters and context values arrive already defined, so nothing in
// this pass opens a range for them.
//
// The zero value is the unset range, which is what `Identifier.MutableRange` holds before this pass
// runs and what a caller must handle.
type MutableRange struct {
	Start static_single_assignment.EvaluationOrder
	End   static_single_assignment.EvaluationOrder
}

// IsSet reports whether a range was ever opened.
func (mutableRange MutableRange) IsSet() bool {
	return mutableRange.Start != 0 || mutableRange.End != 0
}

// Contains reports whether an evaluation position lies inside the range.
//
// Half-open, matching React's `inRange`: the start is inside and the end is not. An unset range
// contains nothing, which is the answer that keeps a caller from having to check IsSet first.
func (mutableRange MutableRange) Contains(order static_single_assignment.EvaluationOrder) bool {
	if !mutableRange.IsSet() {
		return false
	}
	return order >= mutableRange.Start && order < mutableRange.End
}

// IsValid reports whether a range satisfies upstream's invariant.
//
// React's `validateMutableRange` asserts `(start === 0 && end === 0) || end > start`. A range that
// is set but empty or inverted is a bug in whatever produced it, and upstream raises rather than
// tolerating it.
func (mutableRange MutableRange) IsValid() bool {
	if !mutableRange.IsSet() {
		return true
	}
	return mutableRange.End > mutableRange.Start
}
