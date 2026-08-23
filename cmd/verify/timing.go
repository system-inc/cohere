package main

import (
	"fmt"
	"io"
	"time"

	"github.com/system-inc/verify/internal/program"
)

// printTimings renders per-rule cost, most expensive first.
//
// A rule budget you cannot see is a rule budget you cannot hold. Every performance win in the
// migration this tool replaces came from a measurement nobody could have inferred: the worst
// offender cost 2,580ms of a 3,700ms run because it resolved a scope lookup on every identifier
// before testing whether the name even looked abbreviated, and a cheap structural gate took it to
// 152ms with byte-identical findings. Nobody predicted that. A timing table found it.
//
// Setup and listeners are separate columns because they are different defects. Time in setup means
// a rule is doing expensive work to decide it has nothing to do. Time in listeners means it is
// doing expensive work per node. The fixes have nothing in common.
func printTimings(out io.Writer, timings *program.Timings, lintDuration time.Duration) {
	sorted := timings.Sorted()
	if len(sorted) == 0 {
		// A timing table with no rows is the instrument measuring nothing, which must not print as
		// though it measured everything and found it fast.
		fmt.Fprintf(out, "\ntiming: no rules ran, so there is nothing to measure\n")
		return
	}

	// Attributed time is summed across workers, so it exceeds wall clock by roughly the worker
	// count. Reporting it against wall clock would print shares over 100 percent and read as a bug
	// in the measurement rather than as parallelism. Shares are taken against the attributed total,
	// which is the number they are actually a share of.
	attributed := timings.TotalDuration()

	fmt.Fprintf(out, "\ntiming: %d rules, %s of rule time across all workers, %s wall clock\n",
		len(sorted), formatMilliseconds(attributed), formatMilliseconds(lintDuration))
	// Said out loud rather than left for someone to discover: a --timing run is slower than a real
	// one. Each listener call is wrapped in a time.Now pair costing about 42ns against a listener
	// body well under a nanosecond, which is why these numbers are for comparing rules to each
	// other and never for quoting as the tool's speed.
	fmt.Fprintf(out, "  (a --timing run is slower than a real one: every listener call is timed. "+
		"Compare rules to each other, not these totals to a normal run.)\n")
	fmt.Fprintf(out, "  %-42s %9s %9s %9s %8s %8s %8s\n",
		"rule", "total", "setup", "listen", "files", "nodes", "found")

	for _, timing := range sorted {
		share := ""
		if attributed > 0 {
			share = fmt.Sprintf(" %5.1f%%", 100*float64(timing.TotalDuration())/float64(attributed))
		}

		fmt.Fprintf(out, "  %-42s %9s %9s %9s %8d %8d %8d%s\n",
			timing.Name,
			formatMilliseconds(timing.TotalDuration()),
			formatMilliseconds(timing.SetupDuration),
			formatMilliseconds(timing.ListenerDuration),
			timing.FilesListened,
			timing.NodesOffered,
			timing.Findings,
			share,
		)
	}

	printTimingNotes(out, sorted)
}

// printTimingNotes names the two shapes a per-rule table does not make obvious on its own.
//
// The coverage line already reports a rule that listened to nothing. These are the other two the
// brief asked for: a rule that listens to everything, and work duplicated across rules.
func printTimingNotes(out io.Writer, sorted []program.RuleTiming) {
	mostOffered := 0
	for _, timing := range sorted {
		if timing.NodesOffered > mostOffered {
			mostOffered = timing.NodesOffered
		}
	}
	if mostOffered == 0 {
		return
	}

	for _, timing := range sorted {
		// A rule offered nearly every node in the program registered for a very common kind.
		// Returning early from those is legitimate and common, so this is a note rather than a
		// finding. It is also exactly how a rule that looks cheap per call becomes expensive at
		// scale, which is invisible in a total alone.
		if timing.NodesOffered >= mostOffered && timing.FilesListened > 0 {
			fmt.Fprintf(out,
				"  note: %s was offered %d nodes, the most of any rule — it registered for a very common kind\n",
				timing.Name, timing.NodesOffered)
		}
	}

	// Duplicated work is invisible per rule and obvious in aggregate: three rules independently
	// scanning the same comment trivia each look reasonable and together cost three scans. Rules
	// offered the same node count are the candidates, since they registered for the same kinds.
	byOffered := map[int][]string{}
	for _, timing := range sorted {
		if timing.NodesOffered > 0 {
			byOffered[timing.NodesOffered] = append(byOffered[timing.NodesOffered], timing.Name)
		}
	}
	for offered, names := range byOffered {
		if len(names) < 3 {
			continue
		}
		fmt.Fprintf(out,
			"  note: %d rules were each offered exactly %d nodes (%v) — they registered for the same kinds and may be redoing each other's work\n",
			len(names), offered, names)
	}
}

// formatMilliseconds reads at a glance, which is the resolution any of these numbers means anything
// at. Sub-millisecond costs print as a fraction rather than as a bare 0, because a rule at 0.4ms
// and a rule that never ran are different facts.
func formatMilliseconds(duration time.Duration) string {
	milliseconds := float64(duration) / float64(time.Millisecond)
	if milliseconds >= 100 {
		return fmt.Sprintf("%.0fms", milliseconds)
	}
	if milliseconds >= 1 {
		return fmt.Sprintf("%.1fms", milliseconds)
	}
	return fmt.Sprintf("%.2fms", milliseconds)
}
