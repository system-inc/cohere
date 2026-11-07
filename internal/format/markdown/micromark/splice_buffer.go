package micromark

import (
	"fmt"
	"slices"
)

// SpliceBuffer is micromark-util-subtokenize's SpliceBuffer, a gap buffer over events. It keeps a cursor:
// the items before it sit in left in order, and the items from it on sit in right reversed, so the item
// at the cursor is the last of right. Moving the cursor costs the distance it moves, and a splice at the
// cursor costs only what it removes and inserts. Subtokenize splices content in as it sweeps forward, and
// mdast's compile splices list items in as it sweeps each list, so each splice lands near the last one,
// where a plain slice would copy every event after it each time.
//
// Upstream hands its arithmetic straight to Array#splice, which reads a negative number from the end, so
// a negative cursor or a removal past the end does not do what its documentation says. Here each is
// clamped as documented. Neither caller passes such numbers, so the two agree wherever it is called.
type SpliceBuffer struct {
	left  []Event
	right []Event
}

// NewSpliceBuffer is a buffer holding initial, the cursor at its end. Upstream copies initial; here the
// buffer takes it over, since every caller replaces its events with what the buffer gives back. Only what
// subtokenize and mdast's compile call is ported: upstream's push, pop, shift and unshift families are left
// out.
func NewSpliceBuffer(initial []Event) *SpliceBuffer {
	return &SpliceBuffer{left: initial}
}

// Length is one more than the largest index.
func (buffer *SpliceBuffer) Length() int {
	return len(buffer.left) + len(buffer.right)
}

// Get is the item at index, without moving the cursor.
func (buffer *SpliceBuffer) Get(index int) Event {
	if index < 0 || index >= buffer.Length() {
		// Invariant: subtokenize and compile read only below the length they loop to, prepareList's walk
		// back stops at zero, and subcontent's search stops at the event of a linked token, which every
		// linked token has after the one it started from. Upstream throws a RangeError here too; reading
		// on would hand back a wrong event in place of stopping.
		panic(fmt.Sprintf("micromark: cannot access index %d in a splice buffer of size %d", index, buffer.Length()))
	}
	if index < len(buffer.left) {
		return buffer.left[index]
	}
	return buffer.right[len(buffer.right)-index+len(buffer.left)-1]
}

// clampIndex reads index the way Array#slice and Array#splice do: a negative one counts back from the end,
// and the result lies between zero and the length.
func (buffer *SpliceBuffer) clampIndex(index int) int {
	if index < 0 {
		return max(buffer.Length()+index, 0)
	}
	return min(index, buffer.Length())
}

// setCursor moves the cursor to position, clamped between the start and the end, moving the items it
// passes from one stack to the other.
func (buffer *SpliceBuffer) setCursor(position int) {
	clampedPosition := min(max(position, 0), buffer.Length())

	if clampedPosition < len(buffer.left) {
		// Move the cursor back: the items after it go onto right, last first.
		moved := buffer.left[clampedPosition:]
		for index := len(moved) - 1; index >= 0; index-- {
			buffer.right = append(buffer.right, moved[index])
		}
		clear(moved)
		buffer.left = buffer.left[:clampedPosition]
	} else if clampedPosition > len(buffer.left) {
		// Move the cursor forward: the items before it come off the top of right, in order.
		kept := len(buffer.right) - (clampedPosition - len(buffer.left))
		for index := len(buffer.right) - 1; index >= kept; index-- {
			buffer.left = append(buffer.left, buffer.right[index])
		}
		clear(buffer.right[kept:])
		buffer.right = buffer.right[:kept]
	}
}

// Slice is a fresh copy of the items from start up to end, without moving the cursor.
func (buffer *SpliceBuffer) Slice(start int, end int) []Event {
	clampedStart := buffer.clampIndex(start)
	clampedEnd := buffer.clampIndex(end)
	if clampedEnd <= clampedStart {
		return []Event{}
	}

	// All of it before the cursor.
	if clampedEnd <= len(buffer.left) {
		return slices.Clone(buffer.left[clampedStart:clampedEnd])
	}

	result := make([]Event, 0, clampedEnd-clampedStart)
	if clampedStart < len(buffer.left) {
		result = append(result, buffer.left[clampedStart:]...)
	}

	// Right is reversed, so the item at the cursor plus offset sits offset places below its top.
	for index := max(clampedStart, len(buffer.left)); index < clampedEnd; index++ {
		result = append(result, buffer.right[len(buffer.right)-index+len(buffer.left)-1])
	}

	return result
}

// Splice is Array#splice: remove `remove` items at start, insert items there, and return the removed ones.
// The cursor ends after the inserted items. items is copied, never kept.
func (buffer *SpliceBuffer) Splice(start int, remove int, items []Event) []Event {
	buffer.setCursor(buffer.clampIndex(start))

	clampedRemove := min(max(remove, 0), len(buffer.right))
	kept := len(buffer.right) - clampedRemove
	removed := slices.Clone(buffer.right[kept:])
	slices.Reverse(removed)
	clear(buffer.right[kept:])
	buffer.right = buffer.right[:kept]

	buffer.left = append(buffer.left, items...)
	return removed
}
