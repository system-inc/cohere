package main

import (
	"fmt"
	"io"
	"sort"
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

	printSharedFills(out, timings)
	printTimingNotes(out, sorted)
}

// printSharedFills reports work that several rules share, which belongs to none of them.
//
// Without this the first rule to ask for a cached derivation carries its whole cost, and since
// files are walked in parallel that rule is arbitrary. Measured before this existed: three comment
// rules doing identical work reported 171ms, 132ms, and 1.0ms, and the cheap one had simply asked
// last. A reader would have concluded the 171ms rule was expensive and optimized the wrong thing.
func printSharedFills(out io.Writer, timings *program.Timings) {
	fills := timings.SharedFills()
	if len(fills) == 0 {
		return
	}

	keys := make([]string, 0, len(fills))
	for key := range fills {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		fmt.Fprintf(out, "  shared: %s cost %s, paid once per file and used by several rules\n",
			key, formatMilliseconds(fills[key]))
	}
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

	// The rule offered the most nodes is the reference point for every other row, not a warning
	// about that rule. Registering for a common kind and returning early is cheap, so this row shows
	// what a well-gated rule costs at maximum node volume. A rule with comparable nodes and much
	// higher time is doing expensive work per node, and that comparison is what makes the case.
	//
	// This started as a warning and was reworded, because the framing was backwards on the first
	// real run: consistency-no-ambiguous-identifier had the most nodes and was among the cheapest,
	// while a rule with slightly fewer nodes cost sixty times more.
	for _, timing := range sorted {
		if timing.NodesOffered < mostOffered || timing.FilesListened == 0 {
			continue
		}

		perNode := ""
		if timing.NodesOffered > 0 {
			perNode = fmt.Sprintf(", %.0fns per node", float64(timing.TotalDuration().Nanoseconds())/float64(timing.NodesOffered))
		}
		fmt.Fprintf(out,
			"  note: %s saw the most nodes of any rule (%d%s) — compare a rule's cost against this one, not against its own node count\n",
			timing.Name, timing.NodesOffered, perNode)
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
	// A pair counts, not just three or more. The defect that motivated this note was found on a
	// pair: two rules offered exactly 3,407 nodes and differing 83x in cost, where one had filled a
	// shared cache the other read for free. Requiring three would have missed it.
	//
	// The cost spread is what makes it worth printing. Rules registered for the same kind and
	// costing about the same are unremarkable; the same rules an order of magnitude apart mean one
	// of them is carrying work the others are not.
	offeredCounts := make([]int, 0, len(byOffered))
	for offered := range byOffered {
		offeredCounts = append(offeredCounts, offered)
	}
	sort.Ints(offeredCounts)

	for _, offered := range offeredCounts {
		names := byOffered[offered]
		if len(names) < 2 {
			continue
		}

		cheapest, dearest := costRange(sorted, names)
		if cheapest <= 0 || float64(dearest)/float64(cheapest) < 10 {
			continue
		}

		fmt.Fprintf(out,
			"  note: %s and %s were each offered %d nodes but differ %.0fx in cost (%s against %s) — if they share work, this table may be billing it to whichever ran first\n",
			names[0], names[len(names)-1], offered,
			float64(dearest)/float64(cheapest),
			formatMilliseconds(dearest), formatMilliseconds(cheapest))
	}
}

// costRange returns the cheapest and dearest total among the named rules.
//
// Reported as a spread rather than a total because the spread is the signal: equal node counts with
// equal cost is a coincidence, and equal node counts an order of magnitude apart is a rule carrying
// somebody else's work.
func costRange(sorted []program.RuleTiming, names []string) (cheapest, dearest time.Duration) {
	named := map[string]bool{}
	for _, name := range names {
		named[name] = true
	}
	for _, timing := range sorted {
		if !named[timing.Name] {
			continue
		}
		total := timing.TotalDuration()
		if cheapest == 0 || total < cheapest {
			cheapest = total
		}
		if total > dearest {
			dearest = total
		}
	}
	return cheapest, dearest
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
