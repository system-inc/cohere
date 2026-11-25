package go_language

import (
	"math"
	"strings"

	"github.com/system-inc/cohere/internal/go_language/contract"
)

// unreadableMemoryBound is the memory limit cohere-go sets when available memory could not be read, as a bound alone:
// the front door's policy then leaves Go's defaults, no limit and the collector at its default percent, and nothing
// else would stop a load that holds more than expected short of the machine. It is 8 GiB, Kirk's on #4thngsd
// (2026-11-07, "I have 128 GB unified"). It was 5 GiB, and near it the runtime spends CPU returning memory to the
// system and taking it back. Measured as a cap under the policy on a cold run of cohere's root, cohere-go alone, three
// interleaved rounds each, in the slot of 2026-11-07 14:00 (main 4e142795, load 11 to 20):
//
//	policy and a 5 GiB cap:  36.5 to 40.8s user, 5.93 to 6.37s wall, 5.6 GB peak
//	policy and an 8 GiB cap: 34.2 to 35.5s user, 5.37 to 5.57s wall, 8.5 GB peak
const unreadableMemoryBound int64 = 8 << 30

// collectorVariables are the environment variables that set Go's collector, the ones the front door's memory
// policy reads (command/cohere/memory.go).
var collectorVariables = []string{"GOGC", "GOMEMLIMIT"}

// decideMemory says what memory limit cohere-go runs under and where it came from, for the types record, and sets a
// limit only when the front door's policy set none for want of a reading.
//
// cohere-go is the cohere binary run as a child, so it inherits the front door's policy (#nvv3bjy): collection off,
// under a ceiling at half the memory available when the run started. That ceiling is the limit from the first
// allocation, and nothing sets it again: collection is limit-driven, nothing collected until the heap nears it, and the
// extra builds run side by side as far as the width's budget holds them under it (buildsWidth). Half of what is
// available, rather than a share of the machine, is what keeps runs side by side safe: each takes at most half of what
// was free when it started, so the next one sizes itself to what is left.
//
// Measured against a fixed 8 GiB cap that only ever lowered the ceiling, under which the builds ran one at a time
// unless the cap was the limit, and the host's own load collected at 8 GiB on a machine with room past it (#jrr6a5x,
// a land-shaped darwin/arm64 build of cohere's root, 6 rounds a side, rotated):
//
//	Monkee, 10 cores, 16 GB, 11.5 GB available: 5.585 to 5.055 s wall (p 0.004), 3 builds at once under the same
//	                                            5.4 GiB limit the cap's run took one at a time, the same 5.5 GiB peak,
//	                                            no swap, 2.5 s more user
//	an EPYC at 16 and at 64 threads:            0.28 s faster at each, collecting nothing while it works
//	a MacBook under its owner's load:           level within wide ranges, 7 builds at once under a 34.1 GiB limit
//
// The live set cohere's root holds, with check/checker's copy of x/tools' action graph letting go of a result once its
// last reader has run, peaks at about 4.5 GB, which the limit on a 16 GB machine still holds. A repository whose live
// heap is larger than the limit makes the runtime collect hard, up to about half the CPU, to stay near it. That run is
// slower, not wrong, and it is where to look first when a run on a huge repository is slow.
//
// When available memory could not be read the policy leaves Go's defaults: no limit, and the collector at its default
// percent. unreadableMemoryBound is set then as a bound alone, and the collector keeps running as it would.
//
// `GOGC` or `GOMEMLIMIT` in the environment wins, read exactly as the front door's policy reads it, by presence:
// whoever set either has decided about the collector, and cohere-go steps aside.
//
// lookupEnvironment is os.LookupEnv and setMemoryLimit is debug.SetMemoryLimit outside a test. availableBytes is
// the memory the front door's policy read as available in this process, 0 when it could not.
func decideMemory(
	lookupEnvironment func(string) (string, bool),
	setMemoryLimit func(int64) int64,
	availableBytes uint64,
) contract.Memory {
	// A negative limit reads the current one without changing it.
	current := setMemoryLimit(-1)
	var set []string
	for _, name := range collectorVariables {
		if value, present := lookupEnvironment(name); present {
			set = append(set, name+"="+value)
		}
	}
	if len(set) > 0 {
		limit := current
		if limit == math.MaxInt64 {
			limit = 0
		}
		return contract.Memory{
			LimitBytes:  limit,
			Source:      contract.MemorySourceEnvironment,
			Environment: strings.Join(set, " "),
		}
	}
	// No limit with nothing in the environment is the policy's answer to memory it could not read.
	if current == math.MaxInt64 {
		setMemoryLimit(unreadableMemoryBound)
		return contract.Memory{
			LimitBytes: unreadableMemoryBound,
			Source:     contract.MemorySourceUnreadable,
		}
	}
	return contract.Memory{
		LimitBytes:     current,
		Source:         contract.MemorySourceAvailable,
		AvailableBytes: availableBytes,
	}
}
