package goengine

// engineMemoryCap is the most memory the engine lets its heap reach before the collector works to hold it there:
// 5 GiB, so the process's peak, a few hundred megabytes above its heap, stays under the 6 GB budget ruled on
// #a0cgmh6. 5.5 GiB, the cap first ruled, peaked at 6.22 GB in one run of three.
const engineMemoryCap int64 = 5 << 30

// collectorVariables are the environment variables that set Go's collector, the ones the front door's memory
// policy reads (command/cohere/memory.go).
var collectorVariables = []string{"GOGC", "GOMEMLIMIT"}

// capMemory lowers the memory limit the engine inherits to engineMemoryCap, and never raises it.
//
// The engine is the cohere binary run as a child, so it inherits the front door's policy (#nvv3bjy): collection
// off, under a ceiling at half the memory available. That suits a run that allocates and dies, and not this
// one: a full-syntax load of cohere's own workspace peaked at 18 GB under it, and several engines share one
// machine. So the limit comes down to the cap and the collector percent stays as the policy set it, which makes
// collection limit-driven: nothing is collected until the heap nears the cap.
//
// Measured on cohere's own root, the Go child alone, three runs each, with the same 335 findings in every run:
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
