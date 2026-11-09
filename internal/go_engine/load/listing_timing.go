package load

import (
	"github.com/system-inc/cohere/internal/cpu_clock"
	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// measureListing runs work, a piece of the listing, and adds what the calling goroutine's thread spent on it to the
// listing's own CPU. A goroutine the work starts is on another thread, which this clock never sees, so each measures
// its own where it runs. The measurement holds its thread while work waits, so work on goroutines that may be many at
// once, as each package's read is, measures on either side of a wait rather than across it.
func measureListing(collector *timing.Collector, work func()) {
	collector.List(collector.Measure(work), 0)
}

// measureGoCommands runs work, which runs go commands and waits for them, and adds what they spent to the listing's
// children's CPU. The kernel counts a child only once it is waited for, into the process's one total, so a child of
// anything else this process waits for in the same span, another load's go list as parallel tests run them, is counted
// here too: the figure can run over the listing's own commands and never under them. A run that did not ask reads no
// clock.
func measureGoCommands(collector *timing.Collector, work func()) {
	if collector == nil {
		work()
		return
	}
	before, beforeKnown := cpu_clock.Children()
	work()
	after, afterKnown := cpu_clock.Children()
	if beforeKnown && afterKnown {
		collector.List(0, after-before)
	}
}

// measureListingCommand is measureListing and measureGoCommands at once, for work on the calling goroutine that runs a
// go command itself and starts no goroutine that measures its own.
func measureListingCommand(collector *timing.Collector, work func()) {
	measureGoCommands(collector, func() { measureListing(collector, work) })
}
