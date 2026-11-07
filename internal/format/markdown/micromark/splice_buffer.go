package micromark

import (
	"fmt"
	"slices"
)

// spliceBuffer is micromark-util-subtokenize's SpliceBuffer, a gap buffer over events. It keeps a cursor:
// the items before it sit in left in order, and the items from it on sit in right reversed, so the item
// at the cursor is the last of right. Moving the cursor costs the distance it moves, and a splice at the
// cursor costs only what it removes and inserts. Subtokenize splices content in as it sweeps forward, so
// each splice lands near the last one, where a plain slice would copy every event after it each time.
//
// Upstream hands its arithmetic straight to Array#splice, which reads a negative number from the end, so
// a negative cursor or a removal past the end does not do what its documentation says. Here each is
// clamped as documented. Micromark never passes such numbers, so the two agree wherever it calls them.
type spliceBuffer struct {
	left  []Event
	right []Event
}

// newSpliceBuffer is a buffer holding initial, the cursor at its end. Upstream copies initial; here the
// buffer takes it over, since every caller replaces its events with what the buffer gives back.
func newSpliceBuffer(initial []Event) *spliceBuffer {
	return &spliceBuffer{left: initial}
}

// length is one more than the largest index.
func (buffer *spliceBuffer) length() int {
	return len(buffer.left) + len(buffer.right)
}

// get is the item at index, without moving the cursor.
func (buffer *spliceBuffer) get(index int) Event {
	if index < 0 || index >= buffer.length() {
		panic(fmt.Sprintf("micromark: cannot access index %d in a splice buffer of size %d", index, buffer.length()))
	}
	if index < len(buffer.left) {
		return buffer.left[index]
	}
	return buffer.right[len(buffer.right)-index+len(buffer.left)-1]
}

// clampIndex reads index the way Array#slice and Array#splice do: a negative one counts back from the end,
// and the result lies between zero and the length.
func (buffer *spliceBuffer) clampIndex(index int) int {
	if index < 0 {
		return max(buffer.length()+index, 0)
	}
	return min(index, buffer.length())
}

// setCursor moves the cursor to position, clamped between the start and the end, moving the items it
// passes from one stack to the other.
func (buffer *spliceBuffer) setCursor(position int) {
	clampedPosition := min(max(position, 0), buffer.length())

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

// slice is a fresh copy of the items from start up to end, without moving the cursor.
func (buffer *spliceBuffer) slice(start int, end int) []Event {
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

// splice is Array#splice: remove `remove` items at start, insert items there, and return the removed ones.
// The cursor ends after the inserted items. items is copied, never kept.
func (buffer *spliceBuffer) splice(start int, remove int, items []Event) []Event {
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

// pop removes and returns the last item, false when there is none. The cursor moves to the end.
func (buffer *spliceBuffer) pop() (Event, bool) {
	buffer.setCursor(buffer.length())
	if len(buffer.left) == 0 {
		return Event{}, false
	}
	last := len(buffer.left) - 1
	item := buffer.left[last]
	buffer.left[last] = Event{}
	buffer.left = buffer.left[:last]
	return item, true
}

// push adds item at the end. The cursor moves to the end.
func (buffer *spliceBuffer) push(item Event) {
	buffer.setCursor(buffer.length())
	buffer.left = append(buffer.left, item)
}

// pushMany adds items at the end, in order. The cursor moves to the end.
func (buffer *spliceBuffer) pushMany(items []Event) {
	buffer.setCursor(buffer.length())
	buffer.left = append(buffer.left, items...)
}

// shift removes and returns the first item, false when there is none. The cursor moves to the start.
func (buffer *spliceBuffer) shift() (Event, bool) {
	buffer.setCursor(0)
	if len(buffer.right) == 0 {
		return Event{}, false
	}
	last := len(buffer.right) - 1
	item := buffer.right[last]
	buffer.right[last] = Event{}
	buffer.right = buffer.right[:last]
	return item, true
}

// unshift adds item at the start. The cursor moves to the start.
func (buffer *spliceBuffer) unshift(item Event) {
	buffer.setCursor(0)
	buffer.right = append(buffer.right, item)
}

// unshiftMany adds items at the start, in order. The cursor moves to the start. Upstream reverses items
// in place to push them; here they are read backwards, so the caller's slice is left as it was.
func (buffer *spliceBuffer) unshiftMany(items []Event) {
	buffer.setCursor(0)
	for index := len(items) - 1; index >= 0; index-- {
		buffer.right = append(buffer.right, items[index])
	}
}
