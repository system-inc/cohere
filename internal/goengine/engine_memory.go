package goengine

// engineMemoryCap is the most memory the engine lets its heap reach before the collector works to hold it there.
const engineMemoryCap int64 = 6 << 30

// collectorVariables are the environment variables that set Go's collector, the ones the front door's memory
// policy reads (command/cohere/memory.go).
var collectorVariables = []string{"GOGC", "GOMEMLIMIT"}

// capMemory lowers the memory limit the engine inherits to engineMemoryCap, and never raises it.
//
// The engine is the cohere binary run as a child, so it inherits the front door's policy (#nvv3bjy): collection
// off, under a ceiling at half the memory available. That suits a run that allocates and dies, and not this
// one: a full-syntax load of cohere's own workspace peaked at 18 GB under it, and several engines share one
// machine. So the limit comes down to 6 GiB and the collector percent stays as the policy set it, which makes
// collection limit-driven: nothing is collected until the heap nears the cap, so CPU stays near the policy's
// while the peak is bounded.
//
// A repository whose live heap is larger than the cap makes the runtime collect hard, up to about half the CPU,
// to stay near it. That run is slower, not wrong, and it is where to look first when a run on a huge repository
// is slow.
//
// `GOGC` or `GOMEMLIMIT` in the environment wins, read exactly as the front door's policy reads it, by presence:
// whoever set either has decided about the collector, and the engine steps aside.
//
// lookupEnvironment is os.LookupEnv and setMemoryLimit is debug.SetMemoryLimit outside a test.
func capMemory(lookupEnvironment func(string) (string, bool), setMemoryLimit func(int64) int64) {
	for _, name := range collectorVariables {
		if _, set := lookupEnvironment(name); set {
			return
		}
	}
	// A negative limit reads the current one without changing it.
	if current := setMemoryLimit(-1); current > engineMemoryCap {
		setMemoryLimit(engineMemoryCap)
	}
}
