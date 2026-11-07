package micromark

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// spliceBufferModel is a plain slice that does what spliceBuffer should, so each operation on the buffer
// can be checked against it. Its splice is chunked.go's, the one subtokenize used before the buffer.
type spliceBufferModel struct {
	events  []Event
	created int
}

// newEvents is count events, each with its own token, so a misplaced or duplicated one shows.
func (model *spliceBufferModel) newEvents(count int) []Event {
	events := make([]Event, 0, count)
	for range count {
		model.created++
		events = append(events, Event{Enter: model.created%2 == 0, Token: &Token{Type: strconv.Itoa(model.created)}})
	}
	return events
}

// spliceRemoved is what Array#splice removes for start and remove, read before the splice.
func (model *spliceBufferModel) spliceRemoved(start int, remove int) []Event {
	length := len(model.events)
	clampedStart := min(start, length)
	if start < 0 {
		clampedStart = max(length+start, 0)
	}
	clampedRemove := min(max(remove, 0), length-clampedStart)
	return slices.Clone(model.events[clampedStart : clampedStart+clampedRemove])
}

// sliceExpected is what Array#slice returns for start and end.
func (model *spliceBufferModel) sliceExpected(start int, end int) []Event {
	length := len(model.events)
	clamp := func(index int) int {
		if index < 0 {
			return max(length+index, 0)
		}
		return min(index, length)
	}
	if clamp(end) <= clamp(start) {
		return []Event{}
	}
	return slices.Clone(model.events[clamp(start):clamp(end)])
}

// tokenNames lists the tokens of events, for a failure to show which ones differ.
func tokenNames(events []Event) string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, event.Token.Type)
	}
	return "[" + strings.Join(names, " ") + "]"
}

// checkSpliceBuffer fails unless buffer holds exactly what model does, read through get and through slice.
func checkSpliceBuffer(test *testing.T, step string, buffer *spliceBuffer, model *spliceBufferModel) {
	test.Helper()
	if buffer.length() != len(model.events) {
		test.Fatalf("%s: length %d, expected %d", step, buffer.length(), len(model.events))
	}
	for index, expected := range model.events {
		if actual := buffer.get(index); actual != expected {
			test.Fatalf("%s: get(%d) is token %s, expected token %s", step, index, actual.Token.Type,
				expected.Token.Type)
		}
	}
	if actual := buffer.slice(0, buffer.length()); !slices.Equal(actual, model.events) {
		test.Fatalf("%s: slice(0, length) is %s, expected %s", step, tokenNames(actual), tokenNames(model.events))
	}
}

// TestSpliceBufferMatchesPlainSlice runs random operations on a buffer and on a plain slice side by side.
// Starts, counts and cursor positions run past both ends, so the clamping upstream documents is covered.
func TestSpliceBufferMatchesPlainSlice(test *testing.T) {
	test.Parallel()
	random := rand.New(rand.NewPCG(20261107, 1))

	for round := range 200 {
		model := &spliceBufferModel{}
		model.events = model.newEvents(random.IntN(12))
		buffer := newSpliceBuffer(slices.Clone(model.events))

		for operation := range 300 {
			length := len(model.events)
			// Positions from three before the start, read as counting back from the end, to three past it.
			position := func() int { return random.IntN(2*length+7) - length - 3 }
			step := "round " + strconv.Itoa(round) + ", operation " + strconv.Itoa(operation)

			switch random.IntN(10) {
			case 0, 1, 2:
				start := position()
				remove := random.IntN(length+6) - 2
				items := model.newEvents(random.IntN(5))
				expectedRemoved := model.spliceRemoved(start, remove)
				step += ": splice(" + strconv.Itoa(start) + ", " + strconv.Itoa(remove) + ", " +
					strconv.Itoa(len(items)) + " items)"
				removed := buffer.splice(start, remove, items)
				model.events = splice(model.events, start, remove, items)
				if !slices.Equal(removed, expectedRemoved) {
					test.Fatalf("%s: removed %s, expected %s", step, tokenNames(removed), tokenNames(expectedRemoved))
				}
			case 3:
				cursor := position()
				step += ": setCursor(" + strconv.Itoa(cursor) + ")"
				buffer.setCursor(cursor)
				if expected := min(max(cursor, 0), length); len(buffer.left) != expected {
					test.Fatalf("%s: cursor at %d, expected %d", step, len(buffer.left), expected)
				}
			case 4:
				start, end := position(), position()
				step += ": slice(" + strconv.Itoa(start) + ", " + strconv.Itoa(end) + ")"
				if actual := buffer.slice(start, end); !slices.Equal(actual, model.sliceExpected(start, end)) {
					test.Fatalf("%s: differs from Array#slice", step)
				}
			case 5:
				step += ": push"
				items := model.newEvents(1)
				buffer.push(items[0])
				model.events = append(model.events, items[0])
			case 6:
				step += ": pushMany"
				items := model.newEvents(random.IntN(5))
				buffer.pushMany(items)
				model.events = append(model.events, items...)
			case 7:
				step += ": unshift and unshiftMany"
				items := model.newEvents(1 + random.IntN(4))
				given := slices.Clone(items)
				buffer.unshift(items[0])
				buffer.unshiftMany(items[1:])
				model.events = slices.Concat(items[1:], items[:1], model.events)
				if !slices.Equal(items, given) {
					test.Fatalf("%s: changed the items it was given", step)
				}
			case 8:
				step += ": pop"
				item, present := buffer.pop()
				if present != (length > 0) || present && item != model.events[length-1] {
					test.Fatalf("%s: returned the wrong item", step)
				}
				if present {
					model.events = model.events[:length-1]
				}
			case 9:
				step += ": shift"
				item, present := buffer.shift()
				if present != (length > 0) || present && item != model.events[0] {
					test.Fatalf("%s: returned the wrong item", step)
				}
				if present {
					model.events = model.events[1:]
				}
			}

			checkSpliceBuffer(test, step, buffer, model)
		}
	}
}

// TestSpliceBufferEdges pins the cases upstream's documentation names, on a buffer of five.
func TestSpliceBufferEdges(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name   string
		start  int
		remove int
		cursor int
	}{
		{name: "negative start counts back from the end", start: -2, remove: 1, cursor: 3},
		{name: "negative start past the beginning clamps to it", start: -9, remove: 1, cursor: 0},
		{name: "removal past the end stops at it", start: 3, remove: 9, cursor: 3},
		{name: "negative removal removes nothing", start: 2, remove: -1, cursor: 2},
		{name: "start past the end appends", start: 9, remove: 1, cursor: 5},
	}
	for _, testCase := range cases {
		for _, cursorBefore := range []int{-3, 0, 2, 5, 8} {
			model := &spliceBufferModel{}
			model.events = model.newEvents(5)
			buffer := newSpliceBuffer(slices.Clone(model.events))
			buffer.setCursor(cursorBefore)
			if expected := min(max(cursorBefore, 0), 5); len(buffer.left) != expected {
				test.Fatalf("%s: setCursor(%d) left the cursor at %d", testCase.name, cursorBefore, len(buffer.left))
			}

			items := model.newEvents(2)
			step := testCase.name + ", cursor first at " + strconv.Itoa(cursorBefore)
			expectedRemoved := model.spliceRemoved(testCase.start, testCase.remove)
			removed := buffer.splice(testCase.start, testCase.remove, items)
			model.events = splice(model.events, testCase.start, testCase.remove, items)
			if !slices.Equal(removed, expectedRemoved) {
				test.Fatalf("%s: removed %s, expected %s", step, tokenNames(removed), tokenNames(expectedRemoved))
			}
			// The cursor ends after the inserted items.
			if len(buffer.left) != testCase.cursor+len(items) {
				test.Fatalf("%s: cursor at %d, expected %d", step, len(buffer.left), testCase.cursor+len(items))
			}
			checkSpliceBuffer(test, step, buffer, model)
		}
	}
}
