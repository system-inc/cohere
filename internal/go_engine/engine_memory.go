package go_engine

import (
	"math"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// engineMemoryCap is the most memory the engine lets its heap reach before the collector works to hold it there:
// 8 GiB, Kirk's on #4thngsd (2026-11-07, "I have 128 GB unified"). It was 5 GiB, so the peak stayed under the
// 6 GB budget ruled on #a0cgmh6, and near it the runtime spends CPU returning memory to the system and taking it
// back. Measured on a cold run of cohere's root, the engine alone, three interleaved rounds each, in the slot of
// 2026-11-07 14:00 (main 4e142795, load 11 to 20):
//
//	policy and a 5 GiB cap:  36.5 to 40.8s user, 5.93 to 6.37s wall, 5.6 GB peak
//	policy and an 8 GiB cap: 34.2 to 35.5s user, 5.37 to 5.57s wall, 8.5 GB peak
//
// A machine with less memory gets less: the limit the engine inherits is already half the memory available when
// the run started (see capMemory), and the cap only ever lowers it.
const engineMemoryCap int64 = 8 << 30

// collectorVariables are the environment variables that set Go's collector, the ones the front door's memory
// policy reads (command/cohere/memory.go).
var collectorVariables = []string{"GOGC", "GOMEMLIMIT"}

// capMemory lowers the memory limit the engine inherits to engineMemoryCap, never raises it, and says what it
// decided and why, for the types record.
//
// The engine is the cohere binary run as a child, so it inherits the front door's policy (#nvv3bjy): collection
// off, under a ceiling at half the memory available when the run started. That suits a run that allocates and
// dies, and not this one: a full-syntax load of cohere's own workspace peaked at 18 GB under it. So the limit is
// the smaller of the cap and that ceiling, and the collector percent stays as the policy set it, which makes
// collection limit-driven: nothing is collected until the heap nears the limit. Half of what is available, rather
// than a share of the machine, is what keeps runs side by side safe: each takes at most half of what was free when
// it started, so the next one sizes itself to what is left.
//
// When available memory could not be read the policy leaves Go's defaults: no limit, and the collector at its
// default percent. The cap is set then as a bound alone, and the collector keeps running as it would.
//
// Measured at the 5 GiB cap's ruling (#a0cgmh6) on cohere's own root, the Go child alone, three runs each, with the
// same 335 findings in every run:
//
//	policy alone, no cap:                     18.1 GB peak, 29 to 31s user
//	policy and a 6 GiB cap:                   6.5 to 6.7 GB peak, 43 to 44s user
//	policy and a 5.5 GiB cap:                 6.0 to 6.2 GB peak, 47 to 48s user
//	policy and a 5 GiB cap:                   5.6 GB peak, 48 to 52s user (under a load average of 40)
//	5.5 GiB with x/tools' own action graph:   8.3 GB peak, 123s user, thrashing
//
// The last row is why the cap needs check/checker's copy of that graph, which lets go of a result once its last
// reader has run: the upstream graph holds staticcheck's IR for every package until the end, so its live set never
// fits. With the copy the live set peaks at about 4.5 GB: the load's syntax and types (about 1.5 GB), inspect's
// inspectors (0.6 GB) and staticcheck's IR (0.63 GB) among it. Near the cap the collector runs often, which is the
// 1.5 to 1.7 times user CPU; memory was chosen over it, since a run that pushes the machine into swap slows every mind
// on it. Loading dependencies from export data would shrink the load's share and give the CPU back.
//
// A repository whose live heap is larger than the cap makes the runtime collect hard, up to about half the CPU,
// to stay near it. That run is slower, not wrong, and it is where to look first when a run on a huge repository
// is slow.
//
// `GOGC` or `GOMEMLIMIT` in the environment wins, read exactly as the front door's policy reads it, by presence:
// whoever set either has decided about the collector, and the engine steps aside.
//
// lookupEnvironment is os.LookupEnv and setMemoryLimit is debug.SetMemoryLimit outside a test. availableBytes is
// the memory the front door's policy read as available in this process, 0 when it could not.
func capMemory(
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
			CapBytes:    engineMemoryCap,
			Source:      contract.MemorySourceEnvironment,
			Environment: strings.Join(set, " "),
		}
	}
	// No limit with nothing in the environment is the policy's answer to memory it could not read.
	if current == math.MaxInt64 {
		setMemoryLimit(engineMemoryCap)
		return contract.Memory{
			LimitBytes: engineMemoryCap,
			CapBytes:   engineMemoryCap,
			Source:     contract.MemorySourceUnreadable,
		}
	}
	if current >= engineMemoryCap {
		if current > engineMemoryCap {
			setMemoryLimit(engineMemoryCap)
		}
		return contract.Memory{
			LimitBytes:     engineMemoryCap,
			CapBytes:       engineMemoryCap,
			Source:         contract.MemorySourceCap,
			AvailableBytes: availableBytes,
		}
	}
	return contract.Memory{
		LimitBytes:     current,
		CapBytes:       engineMemoryCap,
		Source:         contract.MemorySourceAvailable,
		AvailableBytes: availableBytes,
	}
}
