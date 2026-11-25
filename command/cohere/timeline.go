package main

import (
	"bufio"
	"fmt"

	"github.com/system-inc/cohere/internal/go_language/contract"
)

// timelineLabelWidth is the narrowest a timeline table's step column is, timing's own, so the two tables a run under
// both prints line up. It widens past it to the longest step, a build's indented under its name.
const timelineLabelWidth = timingNameWidth

// printEngineTimeline renders a Go run's timeline record (#3v2zp7g) in printEngineTimings' shape: the unit first, that
// the run had a timed run's shape when it did, then a row per stamp in the order taken, its wall on the engine's clock
// from the moment its package initialized, and the cores busy since the stamp before, the process's user and system CPU
// between the two over the wall between them. A stretch where every core is busy reads near the machine's count, and
// one where the run waited on a lock, a gate or a child reads near 0, which is what a reader of the map is looking for.
//
// A build's stamps are indented under its name, a heading printed each time the rows turn to that build, rather than
// in a column of their own: a build's name runs past thirty characters with its tags, which a column would spend on
// every row, and builds side by side interleave, which the headings show as they happen. A step a load or a check took
// again says its round.
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
		labelWidth = max(labelWidth, len(timelineStep(stamp)))
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
	cores := record.Unavailable == ""
	if cores {
		fmt.Fprintf(out, "  %-*s %9s %7s\n", labelWidth, "step", "wall", "cores")
	} else {
		fmt.Fprintf(out, "  (no CPU this run, because %s. The wall below is real; the cores column is left out rather "+
			"than shown as zero.)\n", record.Unavailable)
		fmt.Fprintf(out, "  %-*s %9s\n", labelWidth, "step", "wall")
	}
	for index, stamp := range record.Stamps {
		if stamp.Build != "" && (index == 0 || record.Stamps[index-1].Build != stamp.Build) {
			fmt.Fprintf(out, "  %s\n", stamp.Build)
		}
		wall := formatMilliseconds(microseconds(stamp.WallMicroseconds))
		if !cores {
			fmt.Fprintf(out, "  %-*s %9s\n", labelWidth, timelineStep(stamp), wall)
			continue
		}
		// The first row has no cores: its wall starts as the engine's package initializes and its CPU at the process's
		// start, so the one over the other is no figure.
		busy := "-"
		if index > 0 {
			busy = coresBusy(record.Stamps[index-1], stamp)
		}
		fmt.Fprintf(out, "  %-*s %9s %7s\n", labelWidth, timelineStep(stamp), wall, busy)
	}
}

// timelineStep is a stamp's step as its row prints it: a build's indented under its name, and a round after the first
// said.
func timelineStep(stamp contract.TimelineStamp) string {
	step := stamp.Label
	if stamp.Round > 0 {
		step = fmt.Sprintf("%s, round %d", step, stamp.Round)
	}
	if stamp.Build != "" {
		step = "  " + step
	}
	return step
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
