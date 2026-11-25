package build_dependence

import (
	"math"
	"strings"
	"testing"
)

// A number past what a unit column or a 32-bit index holds panics, saying so, rather than wrapping into another that
// would be read wrong (#gf9qrzf), and the largest each holds comes back whole: column of -1 and of 2^31, an int32 of
// 2^31, and a uint32 of -1 and of 2^32 panic, and column and an int32 of 2^31-1 and a uint32 of 2^32-1 do not.
func TestANumberPastThirtyTwoBitsPanics(test *testing.T) {
	test.Parallel()

	for _, refused := range []struct {
		name string
		call func()
		says string
	}{
		{"column of -1", func() { _ = column(-1) }, "passes what a unit column holds"},
		{"column of 2^31", func() { _ = column(math.MaxInt32 + 1) }, "passes what a unit column holds"},
		{"int32 of 2^31", func() { _ = narrowed[int32](math.MaxInt32+1, "units") }, "more than int32 holds"},
		{"uint32 of -1", func() { _ = narrowed[uint32](-1, "entries") }, "more than uint32 holds"},
		{"uint32 of 2^32", func() { _ = narrowed[uint32](math.MaxUint32+1, "entries") }, "more than uint32 holds"},
	} {
		if message := panicOf(refused.call); !strings.Contains(message, refused.says) {
			test.Errorf("%s: want a panic saying %q, got %q", refused.name, refused.says, message)
		}
	}
	if got := column(math.MaxInt32); got != math.MaxInt32 {
		test.Errorf("column of 2^31-1: want it whole, got %d", got)
	}
	if got := narrowed[int32](math.MaxInt32, "units"); got != math.MaxInt32 {
		test.Errorf("int32 of 2^31-1: want it whole, got %d", got)
	}
	if got := narrowed[uint32](math.MaxUint32, "entries"); got != math.MaxUint32 {
		test.Errorf("uint32 of 2^32-1: want it whole, got %d", got)
	}
}
