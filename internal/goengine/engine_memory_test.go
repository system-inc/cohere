package goengine

import (
	"math"
	"slices"
	"testing"
)

func TestCapMemory(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name        string
		environment map[string]string
		current     int64
		// set is every limit capMemory set, the read with -1 left out.
		set []int64
	}{
		{name: "no limit is lowered to the cap", current: math.MaxInt64, set: []int64{engineMemoryCap}},
		{name: "a higher limit is lowered to the cap", current: 32 << 30, set: []int64{engineMemoryCap}},
		{name: "a lower limit is never raised", current: 4 << 30},
		{name: "the cap itself is left alone", current: engineMemoryCap},
		{name: "GOGC in the environment wins", environment: map[string]string{"GOGC": "100"}, current: math.MaxInt64},
		{
			name: "GOMEMLIMIT in the environment wins", environment: map[string]string{"GOMEMLIMIT": "20GiB"},
			current: math.MaxInt64,
		},
		{name: "a variable set empty still wins", environment: map[string]string{"GOGC": ""}, current: math.MaxInt64},
	}
	for _, capped := range cases {
		test.Run(capped.name, func(test *testing.T) {
			test.Parallel()
			lookupEnvironment := func(name string) (string, bool) {
				value, set := capped.environment[name]
				return value, set
			}
			limit := capped.current
			var set []int64
			setMemoryLimit := func(requested int64) int64 {
				previous := limit
				if requested >= 0 {
					set = append(set, requested)
					limit = requested
				}
				return previous
			}

			capMemory(lookupEnvironment, setMemoryLimit)

			if !slices.Equal(set, capped.set) {
				test.Errorf("set the limit to %v, want %v", set, capped.set)
			}
		})
	}
}
