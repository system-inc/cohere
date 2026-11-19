package main

import (
	"bufio"
	"fmt"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// timelineLabelWidth is the narrowest a timeline table's label column is, timing's own, so the two tables a run under
// both prints line up. It widens past it to the longest label, a build's being its target's name and a step.
const timelineLabelWidth = timingNameWidth

// printEngineTimeline renders a Go run's timeline record (#3v2zp7g) in printEngineTimings' shape: the unit first, that
// the run had a timed run's shape when it did, then a row per stamp in the order taken, its wall on the engine's clock
// from the process's start, and the cores busy since the stamp before, the process's user and system CPU between the
// two over the wall between them. A stretch where every core is busy reads near the machine's count, and one where the
// run waited on a lock, a gate or a child reads near 0, which is what a reader of the map is looking for.
//
// A record whose CPU could not be read leaves the cores column out rather than print zeros, as the timing table leaves
// its CPU columns out, since no CPU is not zero CPU.
func printEngineTimeline(out *bufio.Writer, record *contract.TimelineRecord) {
	if len(record.Stamps) == 0 {
		fmt.Fprintf(out, "\ntimeline: nothing was stamped\n")
		return
	}
	labelWidth := timelineLabelWidth
	for _, stamp := range record.Stamps {
		labelWidth = max(labelWidth, len(stamp.Label))
	}
	fmt.Fprintf(out, "\ntimeline: when each step began or ended, as wall time since the engine started, and the cores "+
		"busy since the step before, the process's CPU over the wall between them\n")
	if record.Timed {
		fmt.Fprintf(
			out,
			"  (a timed run's shape: --timing turned every cache off and ran the builds one at a time, so "+
				"read the steps against each other, not against a run without it)\n",
		)
	}
	if record.Unavailable != "" {
		fmt.Fprintf(out, "  (no CPU this run, because %s. The wall below is real; the cores column is left out rather "+
			"than shown as zero.)\n", record.Unavailable)
		fmt.Fprintf(out, "  %-*s %9s\n", labelWidth, "step", "wall")
		for _, stamp := range record.Stamps {
			fmt.Fprintf(out, "  %-*s %9s\n", labelWidth, stamp.Label,
				formatMilliseconds(microseconds(stamp.WallMicroseconds)))
		}
		return
	}
	fmt.Fprintf(out, "  %-*s %9s %7s\n", labelWidth, "step", "wall", "cores")
	// The first stamp's cores are since the process's start, whose wall and CPU are both 0.
	previous := contract.TimelineStamp{}
	for _, stamp := range record.Stamps {
		fmt.Fprintf(out, "  %-*s %9s %7s\n", labelWidth, stamp.Label,
			formatMilliseconds(microseconds(stamp.WallMicroseconds)), coresBusy(previous, stamp))
		previous = stamp
	}
}

// coresBusy is how many cores the process kept busy from one stamp to the next, its user and system CPU between them
// over the wall between them, to the hundredth, or `-` where no wall passed, since a share of no time is no number.
func coresBusy(from contract.TimelineStamp, to contract.TimelineStamp) string {
	wall := to.WallMicroseconds - from.WallMicroseconds
	if wall <= 0 {
		return "-"
	}
	cpu := to.UserMicroseconds + to.SystemMicroseconds - from.UserMicroseconds - from.SystemMicroseconds
	return fmt.Sprintf("%.2f", float64(cpu)/float64(wall))
}
