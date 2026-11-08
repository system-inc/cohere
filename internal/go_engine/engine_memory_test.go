package go_engine

import (
	"math"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

func TestCapMemory(test *testing.T) {
	test.Parallel()
	const available uint64 = 41 << 30
	cases := []struct {
		name        string
		environment map[string]string
		current     int64
		// set is every limit capMemory set, the read with -1 left out.
		set  []int64
		want contract.Memory
	}{
		{
			name: "a ceiling above the cap is lowered to the cap", current: 20 << 30, set: []int64{engineMemoryCap},
			want: contract.Memory{
				LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap, Source: contract.MemorySourceCap,
				AvailableBytes: available,
			},
		},
		{
			name: "the cap itself is left alone and named the cap", current: engineMemoryCap,
			want: contract.Memory{
				LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap, Source: contract.MemorySourceCap,
				AvailableBytes: available,
			},
		},
		{
			name: "a ceiling below the cap is never raised", current: 4 << 30,
			want: contract.Memory{
				LimitBytes: 4 << 30, CapBytes: engineMemoryCap, Source: contract.MemorySourceAvailable,
				AvailableBytes: available,
			},
		},
		{
			name: "no limit, from memory the policy could not read, gets the cap as a bound", current: math.MaxInt64,
			set: []int64{engineMemoryCap},
			want: contract.Memory{
				LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap, Source: contract.MemorySourceUnreadable,
			},
		},
		{
			name: "GOGC in the environment wins", environment: map[string]string{"GOGC": "100"}, current: math.MaxInt64,
			want: contract.Memory{
				CapBytes: engineMemoryCap, Source: contract.MemorySourceEnvironment, Environment: "GOGC=100",
			},
		},
		{
			name: "GOMEMLIMIT in the environment wins", environment: map[string]string{"GOMEMLIMIT": "20GiB"},
			current: 20 << 30,
			want: contract.Memory{
				LimitBytes: 20 << 30, CapBytes: engineMemoryCap, Source: contract.MemorySourceEnvironment,
				Environment: "GOMEMLIMIT=20GiB",
			},
		},
		{
			name: "a variable set empty still wins", environment: map[string]string{"GOGC": ""}, current: math.MaxInt64,
			want: contract.Memory{
				CapBytes: engineMemoryCap, Source: contract.MemorySourceEnvironment, Environment: "GOGC=",
			},
		},
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

			availableBytes := available
			if capped.want.Source == contract.MemorySourceUnreadable {
				availableBytes = 0
			}
			decided := capMemory(lookupEnvironment, setMemoryLimit, availableBytes)

			if !slices.Equal(set, capped.set) {
				test.Errorf("set the limit to %v, want %v", set, capped.set)
			}
			if decided != capped.want {
				test.Errorf("decided %+v, want %+v", decided, capped.want)
			}
		})
	}
}
