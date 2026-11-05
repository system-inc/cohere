package program

import (
	"runtime"
	"strings"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// workerMeter measures one walk worker: its thread's CPU across the walk, and each call it makes into a
// rule or a shared derivation.
//
// A call is a frame. Frames nest, since a listener can fill a cached derivation and one fill can ask for
// another, and each frame's own CPU is its clock delta less what the frames inside it took, which are
// billed where they belong. A nil workerMeter is a walk without --timing: every method passes straight
// through, so an ordinary run pays nothing.
type workerMeter struct {
	// timings is the worker's local collector, merged into the walk's when the worker finishes.
	timings *Timings

	// start is the thread's CPU when its walk began.
	start time.Duration

	// frameCost is what one measured call costs with nothing inside it, subtracted from every call.
	frameCost time.Duration

	// nested is the running total of frame deltas, from which each frame reads how much the frames
	// inside it took.
	nested time.Duration

	// depth is how many frames are open, and outermost the summed deltas of the frames opened at depth
	// zero, which is the part of the thread's CPU spent in measured calls.
	depth     int
	outermost time.Duration

	// calls is how many frames closed.
	calls int
}

// finish stops measuring the worker and unlocks its thread.
func (meter *workerMeter) finish() {
	if meter == nil {
		return
	}
	end, _ := threadCpu()
	runtime.UnlockOSThread()

	total := end - meter.start
	account := &meter.timings.Account
	account.WorkersCpu += total
	account.WalkCpu += max(total-meter.outermost, 0)
	account.InstrumentCpu += time.Duration(meter.calls) * meter.frameCost
	account.Calls += meter.calls
}

// open begins a frame.
func (meter *workerMeter) open() frame {
	meter.depth++
	now, _ := threadCpu()
	return frame{start: now, nestedBefore: meter.nested}
}

// close ends a frame and returns its own CPU: its delta, less the frames nested inside it and less what
// measuring it cost. Never negative: a frame cheaper than the measurement's average cost is billed zero.
func (meter *workerMeter) close(opened frame) time.Duration {
	now, _ := threadCpu()
	delta := now - opened.start
	inside := meter.nested - opened.nestedBefore

	// The enclosing frame, if there is one, sees this whole delta as nested work.
	meter.nested = opened.nestedBefore + delta
	meter.depth--
	if meter.depth == 0 {
		meter.outermost += delta
	}
	meter.calls++
	return max(delta-inside-meter.frameCost, 0)
}

// watchFills bills each of a file's cache fills to its derivation rather than to the rule that asked.
func (meter *workerMeter) watchFills(cache *rule.FileCache) {
	if meter == nil {
		return
	}
	cache.SetAroundFill(meter.aroundFill)
}

// setup runs a rule's Run as a frame billed to the rule's setup. The close is deferred because Run can
// panic and be recovered by the rule's containment, and the frames must still balance.
//
// Mutates: timing gains the CPU the rule's Run took, as its setup
func (meter *workerMeter) setup(timing *RuleTiming, run func()) {
	if meter == nil {
		run()
		return
	}
	opened := meter.open()
	defer func() { timing.SetupCpu += meter.close(opened) }()
	run()
}

// call calls a rule's listener on a node, counting the node and billing the call to the rule. Without a
// meter it only calls the listener.
//
// Mutates: timing counts the node offered and gains the CPU the listener took
func (meter *workerMeter) call(timing *RuleTiming, listener func(node *ast.Node), node *ast.Node) {
	if meter == nil {
		listener(node)
		return
	}
	timing.NodesOffered++
	opened := meter.open()
	defer func() { timing.ListenerCpu += meter.close(opened) }()
	listener(node)
}

// aroundFill runs one cache fill as a frame billed to its derivation's family: the key up to its first
// colon, so `hir.Function:175:1001` counts toward `hir.Function`. The rule that asked is billed its
// call less the fill, because the fill is a frame nested in its call.
func (meter *workerMeter) aroundFill(key string, fill func()) {
	family := key
	if colon := strings.IndexByte(key, ':'); colon >= 0 {
		family = key[:colon]
	}
	opened := meter.open()
	defer func() { meter.timings.sharedCpu[family] += meter.close(opened) }()
	fill()
}
