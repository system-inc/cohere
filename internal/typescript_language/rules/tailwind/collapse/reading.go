package tailwind

import (
	"fmt"
	"strings"
)

// Reading is the `{order, count}` pair the class-order comparator sorts on.
//
// Order is the sorted property indices a utility declares, and Count is how many declarations it
// emitted. Both are needed: two utilities can declare the same properties and differ in arity, which
// is exactly what separates `slide-in-from-top-50` from `slide-in-from-top-4`.
//
// The zero Reading is not a valid reading. An absent reading is reported by the boolean every
// lookup returns, never by a sentinel value, because an empty order is itself meaningful: a class
// that declares nothing sorts last rather than first, and a control in Phase 0 got that backwards
// and reported 365,174 spurious mismatches while the model was fine.
type Reading struct {
	// Order holds property indices into the global property order, ascending.
	//
	// It is shared rather than copied. Readings are interned by the generator, so many descriptors
	// point at one Reading and the same backing array; callers must treat it as read-only. Nothing
	// in the port mutates a reading, and the alternative, copying on every lookup, would allocate on
	// the hot path of a rule that runs on every class attribute in the repository.
	Order []int
	// Count is the number of declarations the utility emitted.
	Count int
}

// Equal reports whether two readings are the same reading.
//
// Written out rather than deferring to `slices.Equal` plus a field compare so the two halves of the
// comparison stay visibly symmetric; a reading differing only in Count is the case the fixture
// corpus is thickest around.
func (reading Reading) Equal(other Reading) bool {
	if reading.Count != other.Count || len(reading.Order) != len(other.Order) {
		return false
	}
	for index, value := range reading.Order {
		if value != other.Order[index] {
			return false
		}
	}
	return true
}

// Format renders a reading the way the extractor's `readingKey` does, so a Go failure message and a
// JavaScript one can be compared by eye without translating between two spellings.
func (reading Reading) Format() string {
	parts := make([]string, len(reading.Order))
	for index, order := range reading.Order {
		parts[index] = fmt.Sprint(order)
	}
	return "[" + strings.Join(parts, ",") + "]#" + fmt.Sprint(reading.Count)
}
